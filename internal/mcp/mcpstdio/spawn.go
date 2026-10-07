package mcpstdio

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/mcp/mcperr"
	"github.com/orbitproxy/orbitproxy-go/internal/userenv"
)

// SpawnConfig 描述一个待启动的 stdio MCP server 进程。
//
// FamilyKey / RequiresEnvFile / PreflightEnv 均来自控制面下发的 catalog 元数据，
// 客户端不对任何具体 connector 做特判。
type SpawnConfig struct {
	Command        string   // 命令名或绝对路径，不经 shell
	Args           []string // argv 参数列表
	WorkDir        string   // 子进程工作目录，空则继承父进程
	Env            []string // 显式设置的环境变量（KEY=VALUE 格式）
	EnvPassthrough []string // 从父进程环境透传的变量名白名单
	FamilyKey      string   // 官方 connector family 标识，仅作透传与展示
	// RequiresEnvFile 为 true 时，启动前必须存在 <machineDir>/env/<endpointID>.env 且含至少一对 KEY=VAL。
	RequiresEnvFile bool
	// PreflightEnv 仅在预检 / 发现这类诊断性会话注入子进程（如打开服务端日志开关），运行时会话忽略。
	PreflightEnv []string
}

// WithPreflightEnv 返回合并了 PreflightEnv 的配置副本，供诊断性会话使用。
func WithPreflightEnv(cfg SpawnConfig) SpawnConfig {
	if len(cfg.PreflightEnv) == 0 {
		return cfg
	}
	cfg.Env = mergeEnvPairs(cfg.Env, cfg.PreflightEnv)
	return cfg
}

// Process 是一个已启动的子进程，持有三个管道句柄。
type Process struct {
	Cmd       *exec.Cmd
	Stdin     io.WriteCloser
	Stdout    io.ReadCloser
	Stderr    io.ReadCloser
	StartedAt time.Time

	waitOnce sync.Once
	waitErr  error
}

// PID 返回子进程的操作系统进程 ID。
func (p *Process) PID() int {
	if p.Cmd.Process == nil {
		return 0
	}
	return p.Cmd.Process.Pid
}

// Alive 检查子进程是否仍在运行（ProcessState 为 nil 表示尚未退出）。
func (p *Process) Alive() bool {
	return p.Cmd.ProcessState == nil
}

// Wait 等待子进程退出。可安全重复调用（内部 sync.Once）。
func (p *Process) Wait() error {
	if p == nil || p.Cmd == nil {
		return nil
	}
	p.waitOnce.Do(func() {
		p.waitErr = p.Cmd.Wait()
	})
	return p.waitErr
}

// ExitCode 在 Wait 完成后返回退出码；尚未退出时返回 nil。
func (p *Process) ExitCode() *int {
	if p == nil || p.Cmd == nil || p.Cmd.ProcessState == nil {
		return nil
	}
	code := p.Cmd.ProcessState.ExitCode()
	return &code
}

// Spawn 启动一个 stdio MCP server 子进程。
//
// 设计决策：
//   - 不经 shell（exec.Command 直传 argv），消除命令注入面
//   - Setpgid=true 使子进程独立进程组，避免父进程信号连坐
//   - 环境变量只按 EnvPassthrough 白名单从父进程透传，不继承全部环境
func Spawn(cfg SpawnConfig) (*Process, error) {
	if cfg.Command == "" {
		return nil, mcperr.New(mcperr.StageCommand, mcperr.CodeCommandNotFound, "command is required")
	}

	resolved, err := userenv.LookPath(cfg.Command)
	if err != nil {
		return nil, mcperr.Wrap(mcperr.StageCommand, mcperr.CodeCommandNotFound, "command not found in PATH: "+cfg.Command, err)
	}
	cmd := exec.Command(resolved, cfg.Args...)

	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
	}

	// 构造子进程环境：显式 Env + 按白名单透传
	env := buildEnv(cfg.Env, cfg.EnvPassthrough)
	if len(env) > 0 {
		cmd.Env = env
	}

	// 设置进程组隔离（平台相关部分在 spawn_unix.go / spawn_windows.go）
	setSysProcAttr(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, mcperr.Wrap(mcperr.StageSpawn, mcperr.CodeSpawnFailed, "stdin pipe: "+err.Error(), err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, mcperr.Wrap(mcperr.StageSpawn, mcperr.CodeSpawnFailed, "stdout pipe: "+err.Error(), err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, mcperr.Wrap(mcperr.StageSpawn, mcperr.CodeSpawnFailed, "stderr pipe: "+err.Error(), err)
	}

	if err := cmd.Start(); err != nil {
		return nil, mcperr.Wrap(mcperr.StageSpawn, mcperr.CodeSpawnFailed, "start "+resolved+": "+err.Error(), err)
	}

	return &Process{
		Cmd:       cmd,
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stderr,
		StartedAt: time.Now(),
	}, nil
}

// buildEnv 构造子进程的环境变量列表。
// 策略：先加入显式指定的 Env，再按 passthrough 白名单从当前进程环境中透传。
// 重复的 key 以显式值优先。
func buildEnv(explicit []string, passthrough []string) []string {
	if len(explicit) == 0 && len(passthrough) == 0 {
		return applyUserToolEnv(os.Environ())
	}

	// 收集已显式指定的 key
	seen := make(map[string]bool, len(explicit))
	for _, kv := range explicit {
		if idx := strings.IndexByte(kv, '='); idx > 0 {
			seen[kv[:idx]] = true
		}
	}

	result := make([]string, 0, len(explicit)+len(passthrough))
	result = append(result, explicit...)

	// 按白名单透传，跳过已显式覆盖的。PATH 用用户工具链 PATH。
	for _, name := range passthrough {
		if seen[name] {
			continue
		}
		if name == "PATH" {
			if val := userenv.PATH(); val != "" {
				result = append(result, "PATH="+val)
				seen["PATH"] = true
			}
			continue
		}
		if val, ok := os.LookupEnv(name); ok {
			result = append(result, name+"="+val)
			seen[name] = true
		}
	}

	// PATH 和 HOME 对 Node.js/Python 运行时是必需的。
	// PATH 用用户工具链 PATH（nvm/fnm/volta），不改本进程环境。
	ensureKey := func(key, val string) {
		if val == "" {
			return
		}
		if seen[key] {
			return
		}
		for _, name := range passthrough {
			if name == key {
				return
			}
		}
		result = append(result, key+"="+val)
	}
	ensureKey("PATH", userenv.PATH())
	ensureKey("HOME", userenv.Home())
	ensureKey("npm_config_prefix", userenv.NpmPrefix())

	return result
}

func applyUserToolEnv(env []string) []string {
	if path := userenv.PATH(); path != "" {
		env = replaceEnvKey(env, "PATH", path)
	}
	if home := userenv.Home(); home != "" {
		env = replaceEnvKey(env, "HOME", home)
	}
	if prefix := userenv.NpmPrefix(); prefix != "" {
		env = replaceEnvKey(env, "npm_config_prefix", prefix)
	}
	return env
}

func replaceEnvKey(env []string, key, val string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			if !replaced {
				out = append(out, prefix+val)
				replaced = true
			}
			continue
		}
		out = append(out, kv)
	}
	if !replaced {
		out = append(out, prefix+val)
	}
	return out
}
