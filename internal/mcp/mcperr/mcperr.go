// Package mcperr 定义 MCP 预检 / 发现 / 会话各阶段的稳定错误码与类型化错误。
//
// 错误码是客户端与控制面之间的契约：只增不改。控制面不解析消息文本，只按 Code 渲染文案，
// 证据（退出码、stderr / stdout 尾部）随 Error 一起携带并以固定格式拼入 Error()。
package mcperr

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Stage 标识错误发生在预检的哪一阶段。
type Stage string

const (
	StageCommand      Stage = "command"      // 命令解析：PATH / 可执行位 / 工作目录
	StagePackage      Stage = "package"      // 包是否已安装（npx / uvx）
	StagePrerequisite Stage = "prerequisite" // 运行前提：环境文件等
	StageSpawn        Stage = "spawn"        // 子进程启动
	StageHandshake    Stage = "handshake"    // MCP initialize / initialized
	StageSettle       Stage = "settle"       // 握手后存活窗口
	StageProtocol     Stage = "protocol"     // tools/list 等 JSON-RPC 交互
	StageDial         Stage = "dial"         // forward 型：本地 HTTP 连接
)

// 错误码。与 Stage 的对应关系见包文档；controlplane 侧按这些值渲染文案。
const (
	CodeCommandNotFound      = "command_not_found"
	CodeCommandNotExecutable = "command_not_executable"
	CodePackageNotInstalled  = "package_not_installed"
	CodeEnvFileMissing       = "env_file_missing"
	CodeSpawnFailed          = "spawn_failed"
	CodeHandshakeTimeout     = "handshake_timeout"
	CodeHandshakeRejected    = "handshake_rejected"
	CodeExitedOnStart        = "exited_on_start"        // 握手完成前进程退出
	CodeExitedAfterHandshake = "exited_after_handshake" // 握手成功后存活窗内退出
	CodeExitedAtRuntime      = "exited_at_runtime"      // 运行期退出（会话层使用）
	CodeProtocolError        = "protocol_error"
	CodeToolsListRejected    = "tools_list_rejected"
	CodeDialFailed           = "dial_failed"
	CodeHTTPStatus           = "http_status"
	CodeTimeout              = "timeout"
	CodePingTimeout          = "ping_timeout"
	CodeConcurrencyLimit     = "concurrency_limit"
	CodeInternal             = "internal"
)

// 证据字段在 Error() 中的分隔符与键名。控制台按同样的约定解析。
const (
	evidenceSep    = "; "
	keyExitCode    = "exit_code="
	keyStderr      = "stderr="
	keyStdout      = "stdout="
	emptyEvidence  = "(empty)"
	maxEvidenceLen = 512
)

// Error 是带阶段、错误码与进程证据的预检错误。
type Error struct {
	Stage   Stage
	Code    string
	Message string
	// ExitCode 非 nil 表示子进程已退出；此时即使 StderrTail 为空也会输出 stderr=(empty)。
	ExitCode   *int
	StderrTail string
	StdoutTail string
	Cause      error
}

// New 构造一个不带进程证据的错误。
func New(stage Stage, code, message string) *Error {
	return &Error{Stage: stage, Code: code, Message: message}
}

// Wrap 构造一个包装底层错误的错误；Message 为空时使用 cause 文本。
func Wrap(stage Stage, code, message string, cause error) *Error {
	if message == "" && cause != nil {
		message = cause.Error()
	}
	return &Error{Stage: stage, Code: code, Message: message, Cause: cause}
}

// WithProcess 附加子进程退出证据。
func (e *Error) WithProcess(exitCode *int, stderrTail, stdoutTail string) *Error {
	e.ExitCode = exitCode
	e.StderrTail = stderrTail
	e.StdoutTail = stdoutTail
	return e
}

// Error 输出固定格式：<Message>; exit_code=N; stderr=<tail|(empty)>; stdout=<tail>。
// 只在对应证据存在时追加分段。
func (e *Error) Error() string {
	parts := []string{e.Message}
	if e.ExitCode != nil {
		parts = append(parts, fmt.Sprintf("%s%d", keyExitCode, *e.ExitCode))
		stderr := tail(e.StderrTail)
		if stderr == "" {
			stderr = emptyEvidence
		}
		parts = append(parts, keyStderr+stderr)
	} else if stderr := tail(e.StderrTail); stderr != "" {
		parts = append(parts, keyStderr+stderr)
	}
	if stdout := tail(e.StdoutTail); stdout != "" {
		parts = append(parts, keyStdout+stdout)
	}
	return strings.Join(parts, evidenceSep)
}

// Unwrap 支持 errors.Is / errors.As 穿透到底层错误。
func (e *Error) Unwrap() error { return e.Cause }

// ProcessExited 报告该错误是否携带子进程退出证据。
func (e *Error) ProcessExited() bool { return e != nil && e.ExitCode != nil }

// As 从任意 error 中取出 *Error。
func As(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

// CodeOf 返回错误对应的稳定错误码；非 *Error 时按通用规则归类。
func CodeOf(err error) string {
	code, _ := Classify(err)
	return code
}

// Classify 把任意错误映射为 (error_code, error_message)。
// *Error 直接取 Code；context 超时 → timeout；其余 → internal。不做文本匹配。
func Classify(err error) (code, message string) {
	if err == nil {
		return CodeInternal, "unknown error"
	}
	if typed, ok := As(err); ok {
		return typed.Code, err.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CodeTimeout, err.Error()
	}
	return CodeInternal, err.Error()
}

// IsProcessExit 报告错误是否表示子进程已退出（任一退出类错误码）。
func IsProcessExit(err error) bool {
	typed, ok := As(err)
	if !ok {
		return false
	}
	switch typed.Code {
	case CodeExitedOnStart, CodeExitedAfterHandshake, CodeExitedAtRuntime:
		return true
	}
	return typed.ExitCode != nil
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxEvidenceLen {
		s = s[len(s)-maxEvidenceLen:]
	}
	return s
}
