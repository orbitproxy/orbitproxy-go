package mcpstdio

import (
	"regexp"
	"sync"
	"time"
)

// ----------------------------------------------------------------
// 错误码常量——稳定契约，只增不改
// Machine 上报错误码，控制面渲染人话和修复指引
// ----------------------------------------------------------------

const (
	CodeCommandNotFound      = "command_not_found"
	CodeCommandNotExecutable = "command_not_executable"
	CodeSpawnFailed          = "spawn_failed"
	CodeExitedOnStart        = "exited_on_start"        // 启动后 2s 内退出
	CodeHandshakeTimeout     = "handshake_timeout"
	CodeHandshakeRejected    = "handshake_rejected"
	CodeExitedAtRuntime      = "exited_at_runtime"       // 运行中退出
	CodePingTimeout          = "ping_timeout"
	CodeConcurrencyLimit     = "concurrency_limit"
	CodeInternal             = "internal"
)

// 启动后存活时长阈值，低于此值判定为启动失败（而非运行时故障）
const startupSurvivalThreshold = 2 * time.Second

// Diagnostic is structured evidence for a passive health observation
// (process exit / handshake failure). Callers should treat it as input to
// the unified health model (MarkUnhealthy), not a separate product concept.
type Diagnostic struct {
	EndpointID string            `json:"endpoint_id"`
	Code       string            `json:"code"`
	Message    string            `json:"message,omitempty"`
	ExitCode   *int              `json:"exit_code,omitempty"`
	DurationMs int64             `json:"duration_ms,omitempty"`
	StderrTail []byte            `json:"stderr_tail,omitempty"`
	Fields     map[string]string `json:"fields,omitempty"`
	OccurCount int               `json:"occur_count,omitempty"`
	FirstSeenTs int64            `json:"first_seen_ts,omitempty"`
	OccurredAt time.Time         `json:"occurred_at"`
}

// DiagnosticCallback 是诊断事件回调。
// SDK 只负责调用，上报到控制面的逻辑在 Machine 层实现。
type DiagnosticCallback func(Diagnostic)

// ----------------------------------------------------------------
// ClassifyExit 根据进程退出信息分级判定
// ----------------------------------------------------------------

// ClassifyExit 对已退出的子进程进行诊断分级。
// 参数：
//   - endpointID: 关联的 endpoint
//   - proc: 已退出的进程（ProcessState 非 nil）
//   - handshakeErr: 握手阶段错误（可能为 nil）
//   - stderrBuf: stderr 环形缓冲（可能为 nil）
func ClassifyExit(endpointID string, proc *Process, handshakeErr error, stderrBuf *StderrBuffer) Diagnostic {
	diag := Diagnostic{
		EndpointID: endpointID,
		OccurredAt: time.Now(),
		OccurCount: 1,
	}

	if proc.Cmd.ProcessState != nil {
		exitCode := proc.Cmd.ProcessState.ExitCode()
		diag.ExitCode = &exitCode
	}

	duration := time.Since(proc.StartedAt)
	diag.DurationMs = duration.Milliseconds()

	if stderrBuf != nil {
		diag.StderrTail = Sanitize(stderrBuf.Tail())
	}

	// L2: 按存活时长区分启动失败 vs 运行时故障
	if duration < startupSurvivalThreshold {
		diag.Code = CodeExitedOnStart
		diag.Message = "process exited within 2s of start"
		return diag
	}

	// L3: 握手失败
	if handshakeErr != nil {
		diag.Code = CodeHandshakeRejected
		diag.Message = handshakeErr.Error()
		return diag
	}

	// 运行时退出
	diag.Code = CodeExitedAtRuntime
	diag.Message = "process exited during operation"
	return diag
}

// ----------------------------------------------------------------
// StderrBuffer：固定大小环形缓冲，只保留尾部内容
// ----------------------------------------------------------------

const stderrBufSize = 4 * 1024 // 4KB

// StderrBuffer 是一个固定大小的环形缓冲区，用于捕获子进程 stderr 的尾部输出。
// 设计约束：
//   - 只保留最近 4KB（崩溃原因通常在最后）
//   - 避免刷屏进程打爆内存
//   - 实现 io.Writer 接口，可直接 io.Copy 从 stderr 管道
type StderrBuffer struct {
	mu  sync.Mutex
	buf []byte
	pos int  // 下一个写入位置
	full bool // 缓冲区是否已写满过一轮
}

// NewStderrBuffer 创建 4KB 的 stderr 环形缓冲。
func NewStderrBuffer() *StderrBuffer {
	return &StderrBuffer{
		buf: make([]byte, stderrBufSize),
	}
}

// Write 实现 io.Writer，将 stderr 输出写入环形缓冲。
func (sb *StderrBuffer) Write(p []byte) (n int, err error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	n = len(p)
	for len(p) > 0 {
		space := stderrBufSize - sb.pos
		if space >= len(p) {
			copy(sb.buf[sb.pos:], p)
			sb.pos += len(p)
			if sb.pos == stderrBufSize {
				sb.pos = 0
				sb.full = true
			}
			break
		}
		copy(sb.buf[sb.pos:], p[:space])
		p = p[space:]
		sb.pos = 0
		sb.full = true
	}
	return n, nil
}

// Tail 返回缓冲区中最近的内容（按写入顺序排列）。
func (sb *StderrBuffer) Tail() []byte {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	if !sb.full {
		out := make([]byte, sb.pos)
		copy(out, sb.buf[:sb.pos])
		return out
	}
	// 环形缓冲：pos 之后的是旧数据（但先写入），pos 之前是新数据
	out := make([]byte, stderrBufSize)
	copy(out, sb.buf[sb.pos:])
	copy(out[stderrBufSize-sb.pos:], sb.buf[:sb.pos])
	return out
}

// ----------------------------------------------------------------
// Sanitize：脱敏，打码常见凭证模式
// ----------------------------------------------------------------

var sanitizePatterns = []*regexp.Regexp{
	// URL 中的用户名密码：scheme://user:pass@host
	regexp.MustCompile(`://[^:@/\s]+:[^@/\s]+@`),
	// Bearer token
	regexp.MustCompile(`(?i)bearer\s+\S+`),
	// OpenAI / Anthropic 风格 API key
	regexp.MustCompile(`(?i)(sk-|api[_-]?key[=:\s]+)\S{10,}`),
	// 连续 32+ 位 hex/base64 随机串（常见 token/secret 格式）
	regexp.MustCompile(`[A-Za-z0-9+/=_-]{32,}`),
}

var sanitizeReplacements = []string{
	"://***:***@",
	"Bearer [REDACTED]",
	"${1}[REDACTED]",
	"[REDACTED]",
}

// Sanitize 对 stderr 文本做最大努力的凭证脱敏。
// 注意：脱敏不保证完备，因此 stderr 默认不出用户机器。
// 脱敏仅作为次级防线，主防线是「默认不上报」。
func Sanitize(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}
	result := make([]byte, len(raw))
	copy(result, raw)
	for i, pat := range sanitizePatterns {
		result = pat.ReplaceAll(result, []byte(sanitizeReplacements[i]))
	}
	return result
}

// ----------------------------------------------------------------
// Deduplicator：同 endpoint_id + error_code 在时间窗口内合并
// ----------------------------------------------------------------

type deduplicatorKey struct {
	EndpointID string
	Code       string
}

type deduplicatorEntry struct {
	first    time.Time
	last     time.Time
	count    int
	lastDiag Diagnostic
}

// Deduplicator 在时间窗口内对相同 endpoint + 错误码的诊断事件做去重合并。
// 避免命令配置错误时每次新会话 spawn 失败都产生一条上报。
type Deduplicator struct {
	mu      sync.Mutex
	window  time.Duration
	entries map[deduplicatorKey]*deduplicatorEntry
}

// NewDeduplicator 创建诊断去重器，window 是合并时间窗口。
func NewDeduplicator(window time.Duration) *Deduplicator {
	return &Deduplicator{
		window:  window,
		entries: make(map[deduplicatorKey]*deduplicatorEntry),
	}
}

// Record 记录一个诊断事件，返回是否应该上报（首次或超出窗口）。
// 如果在窗口内已有同类事件，更新计数并返回 false。
// 返回的 Diagnostic 包含合并后的 OccurCount 和 FirstSeenTs。
func (d *Deduplicator) Record(diag Diagnostic) (merged Diagnostic, shouldReport bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := deduplicatorKey{EndpointID: diag.EndpointID, Code: diag.Code}
	now := time.Now()

	entry, exists := d.entries[key]
	if !exists || now.Sub(entry.last) > d.window {
		// 首次出现或窗口已过，新建记录
		d.entries[key] = &deduplicatorEntry{
			first:    now,
			last:     now,
			count:    1,
			lastDiag: diag,
		}
		diag.OccurCount = 1
		diag.FirstSeenTs = now.UnixMilli()
		return diag, true
	}

	// 窗口内重复，更新计数
	entry.count++
	entry.last = now
	entry.lastDiag = diag
	diag.OccurCount = entry.count
	diag.FirstSeenTs = entry.first.UnixMilli()
	return diag, false
}
