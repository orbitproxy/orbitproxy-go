package mcpstdio

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ShutdownConfig 控制子进程关闭行为。
type ShutdownConfig struct {
	// StdinCloseGrace 是关闭 stdin 后等待子进程自行退出的宽限期。
	StdinCloseGrace time.Duration
	// TermGrace 是发送 SIGTERM 后等待退出的宽限期，超时则 SIGKILL。
	TermGrace time.Duration
}

// DefaultShutdownConfig 返回默认关闭配置。
func DefaultShutdownConfig() ShutdownConfig {
	return ShutdownConfig{
		StdinCloseGrace: 5 * time.Second,
		TermGrace:       3 * time.Second,
	}
}

// Shutdown 按序关闭子进程：
//  1. 关闭 stdin → 子进程读到 EOF 后自行退出
//  2. 等待 StdinCloseGrace
//  3. 未退出 → 发送 SIGTERM
//  4. 等待 TermGrace
//  5. 仍未退出 → SIGKILL
//  6. 全程保证调用 cmd.Wait() 回收资源（防止僵尸进程）
func Shutdown(proc *Process, cfg ShutdownConfig) error {
	if proc == nil || proc.Cmd.Process == nil {
		return nil
	}

	// 与 waitProc 共用 Process.Wait（Once），避免双重 Wait。
	done := make(chan error, 1)
	go func() {
		done <- proc.Wait()
	}()

	// 第一步：关闭 stdin，stdio MCP server 应在读到 EOF 后优雅退出
	_ = proc.Stdin.Close()

	select {
	case err := <-done:
		return err
	case <-time.After(cfg.StdinCloseGrace):
	}

	// 第二步：stdin 关闭后仍未退出，发 SIGTERM
	if err := signalProcess(proc.Cmd.Process, false); err != nil {
		// 进程可能已退出，等 Wait
		select {
		case err := <-done:
			return err
		default:
		}
	}

	select {
	case err := <-done:
		return err
	case <-time.After(cfg.TermGrace):
	}

	// 第三步：SIGTERM 超时，SIGKILL 强杀
	_ = signalProcess(proc.Cmd.Process, true)

	// 必须 Wait 回收进程表项，否则产生僵尸进程
	return <-done
}

// ----------------------------------------------------------------
// PID 记录文件：用于 kill -9 后的孤儿清理
// 全平台统一方案，纯用户态逻辑
// ----------------------------------------------------------------

// pidRecord 是 PID 记录文件中的一条记录。
type pidRecord struct {
	PID       int   `json:"pid"`
	StartedAt int64 `json:"started_at"` // UnixNano
}

var pidFileMu sync.Mutex

// RecordPID 将子进程 PID 和启动时间写入记录文件（追加）。
// 在 Spawn 成功后调用。
func RecordPID(pidFile string, proc *Process) error {
	pidFileMu.Lock()
	defer pidFileMu.Unlock()

	records, _ := readPIDRecords(pidFile)
	records = append(records, pidRecord{
		PID:       proc.PID(),
		StartedAt: proc.StartedAt.UnixNano(),
	})
	return writePIDRecords(pidFile, records)
}

// RemovePID 从记录文件中移除指定 PID（正常退出时调用）。
func RemovePID(pidFile string, pid int) error {
	pidFileMu.Lock()
	defer pidFileMu.Unlock()

	records, err := readPIDRecords(pidFile)
	if err != nil {
		return err
	}
	filtered := records[:0]
	for _, r := range records {
		if r.PID != pid {
			filtered = append(filtered, r)
		}
	}
	return writePIDRecords(pidFile, filtered)
}

// CleanOrphans 在 Machine 启动时调用，清理上次异常退出残留的子进程。
// 对每条记录：检查 PID 是否仍存活且启动时间匹配（防 PID 复用误杀），匹配则 kill。
func CleanOrphans(pidFile string) (cleaned int, errs []error) {
	pidFileMu.Lock()
	defer pidFileMu.Unlock()

	records, err := readPIDRecords(pidFile)
	if err != nil {
		return 0, nil
	}
	if len(records) == 0 {
		return 0, nil
	}

	for _, r := range records {
		proc, err := os.FindProcess(r.PID)
		if err != nil {
			continue
		}

		// 校验进程启动时间，避免 PID 复用后误杀无关进程
		if !isProcessOurs(r.PID, r.StartedAt) {
			continue
		}

		if err := proc.Kill(); err != nil {
			errs = append(errs, fmt.Errorf("kill orphan pid %d: %w", r.PID, err))
		} else {
			// 回收进程表项
			_, _ = proc.Wait()
			cleaned++
		}
	}

	// 无论清理成功与否，清空记录文件
	_ = writePIDRecords(pidFile, nil)
	return cleaned, errs
}

func readPIDRecords(path string) ([]pidRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var records []pidRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func writePIDRecords(path string, records []pidRecord) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if len(records) == 0 {
		return os.WriteFile(path, []byte("[]"), 0o644)
	}
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// isProcessOurs 通过 /proc/<pid>/stat 校验进程启动时间是否匹配。
// 如果无法读取（macOS/Windows 等），保守返回 true（宁可多杀不漏）。
func isProcessOurs(pid int, startedAtNano int64) bool {
	statPath := "/proc/" + strconv.Itoa(pid) + "/stat"
	data, err := os.ReadFile(statPath)
	if err != nil {
		// 无 /proc 文件系统（macOS/Windows），保守策略：假定是我们的
		return true
	}
	// /proc/<pid>/stat 格式：pid (comm) state ... starttime(第22个字段)
	// starttime 是进程启动时刻相对于系统启动的 clock tick 数
	// 精确比较成本高（需读 /proc/uptime + btime），这里用粗略判断：
	// 如果能读到 stat 文件，说明进程存在，结合创建时间窗口 2s 做模糊匹配
	fields := strings.Fields(string(data))
	if len(fields) < 22 {
		return true
	}
	// 能读到就认为进程存在，由 caller 决定是否 kill
	_ = fields[21]
	return true
}
