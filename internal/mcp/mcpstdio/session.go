package mcpstdio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// postHandshakeSettle is how long we require the subprocess to stay alive
// after a successful MCP handshake (catches async startup suicides).
const postHandshakeSettle = 2 * time.Second

// SessionConfig 控制单个会话的行为参数。
type SessionConfig struct {
	SpawnConfig                    // 子进程启动配置
	HandshakeTimeout time.Duration // MCP 握手超时（initialize + initialized），默认 30s
	RequestTimeout   time.Duration // 单次 JSON-RPC 请求超时，默认 30s
	EndpointID       string        // 关联的 endpoint ID（用于诊断上报）
	MachineDir       string        // <workdir>/<machineKey>，用于读 env/<endpointID>.env
	PIDFile          string        // PID 记录文件路径
	DiagCallback     DiagnosticCallback
}

// Session 是一个活跃的 stdio MCP 会话。
// 一个 Session = 一个子进程 + 一次 MCP 握手 + 请求路由。
type Session struct {
	mu            sync.RWMutex
	proc          *Process
	writer        *Writer
	reader        *Reader
	pending       *PendingMap
	stderrBuf     *StderrBuffer
	cfg           SessionConfig
	lastActivity  atomic.Int64 // UnixNano，用于空闲超时判断
	closed        atomic.Bool
	handshakeDone bool
	handshakeErr  error
	serverName    string
	serverVersion string
	initResult    json.RawMessage // cached initialize result for client-facing synth

	// 子进程退出通知
	exitCh  chan struct{}
	exitErr error
}

// NewSession 创建一个新会话：启动子进程 → 完成 MCP 握手 → 短窗口确认仍存活。
// 握手失败时会自动关闭子进程并返回错误。
func NewSession(cfg SessionConfig) (*Session, error) {
	if cfg.HandshakeTimeout <= 0 {
		cfg.HandshakeTimeout = 30 * time.Second
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 30 * time.Second
	}

	cfg.SpawnConfig = ApplyEndpointEnvFile(cfg.SpawnConfig, cfg.MachineDir, cfg.EndpointID)

	proc, err := Spawn(cfg.SpawnConfig)
	if err != nil {
		return nil, err
	}

	// 记录 PID 到本地文件（供孤儿清理）
	if cfg.PIDFile != "" {
		_ = RecordPID(cfg.PIDFile, proc)
	}

	s := &Session{
		proc:      proc,
		writer:    NewWriter(proc.Stdin),
		reader:    NewReader(proc.Stdout),
		pending:   NewPendingMap(),
		stderrBuf: NewStderrBuffer(),
		cfg:       cfg,
		exitCh:    make(chan struct{}),
	}
	s.touchActivity()

	// 后台消费 stderr
	go s.drainStderr()
	// 后台消费 stdout，按 id 分发响应
	go s.readLoop()
	// 后台等待进程退出（唯一 Wait 归属之一，与 Shutdown 共用 Process.Wait Once）
	go s.waitProc()

	// MCP 握手
	if err := s.handshake(); err != nil {
		s.handshakeErr = err
		// 握手失败仍要正常关闭子进程
		_ = s.Close()

		// 上报诊断
		if cfg.DiagCallback != nil {
			diag := ClassifyExit(cfg.EndpointID, proc, err, s.stderrBuf)
			if diag.Code == CodeExitedAtRuntime {
				diag.Code = CodeHandshakeRejected
				diag.Message = err.Error()
			}
			cfg.DiagCallback(diag)
		}
		return nil, fmt.Errorf("mcp handshake: %w", err)
	}
	s.handshakeDone = true

	if err := s.waitSettled(postHandshakeSettle); err != nil {
		_ = s.Close()
		if cfg.DiagCallback != nil {
			diag := ClassifyExit(cfg.EndpointID, proc, err, s.stderrBuf)
			if diag.Code == CodeExitedAtRuntime {
				diag.Code = CodeExitedOnStart
				diag.Message = err.Error()
			}
			cfg.DiagCallback(diag)
		}
		return nil, err
	}
	return s, nil
}

// Call 发送一个 JSON-RPC 请求并等待响应。
// 线程安全——多个 goroutine 可并发调用。
func (s *Session) Call(ctx context.Context, msg *Message) (*Message, error) {
	if s.closed.Load() {
		return nil, fmt.Errorf("session closed")
	}
	if !s.Alive() {
		return nil, s.enrichDeadError(fmt.Errorf("subprocess already exited"))
	}
	s.touchActivity()

	// 注册等待并重写 id
	internalID, wait := s.pending.Register(msg.ID, s.cfg.RequestTimeout)
	outMsg := &Message{
		JSONRPC: msg.JSONRPC,
		ID:      internalID,
		Method:  msg.Method,
		Params:  msg.Params,
	}

	if err := s.writer.Write(outMsg); err != nil {
		return nil, s.enrichDeadError(fmt.Errorf("write stdin: %w", err))
	}

	select {
	case resp, ok := <-wait:
		if !ok {
			// chan 被关闭：超时或会话关闭
			if !s.Alive() {
				return nil, s.enrichDeadError(fmt.Errorf("subprocess exited while waiting for response"))
			}
			return nil, fmt.Errorf("request timeout or session closed")
		}
		s.touchActivity()
		return resp, nil
	case <-s.exitCh:
		return nil, s.enrichDeadError(fmt.Errorf("subprocess exited while waiting for response"))
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// SendNotification 向子进程发送一条通知（无 id，无需等待响应）。
func (s *Session) SendNotification(msg *Message) error {
	if s.closed.Load() {
		return fmt.Errorf("session closed")
	}
	if !s.Alive() {
		return s.enrichDeadError(fmt.Errorf("subprocess already exited"))
	}
	s.touchActivity()
	if err := s.writer.Write(msg); err != nil {
		return s.enrichDeadError(err)
	}
	return nil
}

// Close 关闭会话：停止子进程并清理资源。
func (s *Session) Close() error {
	if s.closed.Swap(true) {
		return nil
	}

	// 关闭所有等待中的请求
	s.pending.CloseAll()

	// 按序关闭子进程
	err := Shutdown(s.proc, DefaultShutdownConfig())

	// 清理 PID 记录
	if s.cfg.PIDFile != "" {
		_ = RemovePID(s.cfg.PIDFile, s.proc.PID())
	}

	return err
}

// LastActivity 返回最近一次活动的时间。
func (s *Session) LastActivity() time.Time {
	return time.Unix(0, s.lastActivity.Load())
}

// Alive 返回子进程是否仍在运行。
func (s *Session) Alive() bool {
	if s.closed.Load() {
		return false
	}
	select {
	case <-s.exitCh:
		return false
	default:
		return true
	}
}

// ServerInfo 返回 MCP initialize 握手中获取的 server 信息。
func (s *Session) ServerInfo() (name, version string) {
	return s.serverName, s.serverVersion
}

// CachedInitializeResult 返回握手时缓存的 initialize result（供对客户端合成应答）。
func (s *Session) CachedInitializeResult() json.RawMessage {
	return s.initResult
}

// StderrTail 返回子进程 stderr 环形缓冲尾部。
func (s *Session) StderrTail() []byte {
	if s.stderrBuf == nil {
		return nil
	}
	return s.stderrBuf.Tail()
}

// ExitCode 在进程已退出时返回退出码。
func (s *Session) ExitCode() *int {
	if s.proc == nil {
		return nil
	}
	return s.proc.ExitCode()
}

// enrichDeadError 在进程已死时附带 exit code / stderr，便于请求侧排查。
func (s *Session) enrichDeadError(err error) error {
	if err == nil {
		return nil
	}
	parts := []string{err.Error()}
	if code := s.ExitCode(); code != nil {
		parts = append(parts, fmt.Sprintf("exit_code=%d", *code))
	}
	if tail := strings.TrimSpace(string(Sanitize(s.StderrTail()))); tail != "" {
		if len(tail) > 512 {
			tail = tail[len(tail)-512:]
		}
		parts = append(parts, "stderr="+tail)
	}
	return fmt.Errorf("%s", strings.Join(parts, "; "))
}

// ----------------------------------------------------------------
// 内部方法
// ----------------------------------------------------------------

// handshake 完成 MCP 握手：initialize → notifications/initialized
func (s *Session) handshake() error {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.HandshakeTimeout)
	defer cancel()

	// 发送 initialize
	initReq := &Message{
		JSONRPC: json.RawMessage(`"2.0"`),
		ID:      json.RawMessage(`1`),
		Method:  json.RawMessage(`"initialize"`),
	}
	initParams := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "orbitproxy-go",
			"version": "1.0.0",
		},
	}
	paramsBytes, _ := json.Marshal(initParams)
	initReq.Params = paramsBytes

	resp, err := s.Call(ctx, initReq)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	// 解析 server info
	if len(resp.Error) > 0 && string(resp.Error) != "null" {
		return fmt.Errorf("initialize rejected: %s", string(resp.Error))
	}
	if len(resp.Result) > 0 {
		s.initResult = append(json.RawMessage(nil), resp.Result...)
		var result struct {
			ServerInfo struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
		}
		if err := json.Unmarshal(resp.Result, &result); err == nil {
			s.serverName = result.ServerInfo.Name
			s.serverVersion = result.ServerInfo.Version
		}
	}

	// 发送 notifications/initialized（通知，无需响应）
	notif := &Message{
		JSONRPC: json.RawMessage(`"2.0"`),
		Method:  json.RawMessage(`"notifications/initialized"`),
	}
	return s.SendNotification(notif)
}

// waitSettled 要求握手后进程在窗口内持续存活（抓异步 startup exit）。
func (s *Session) waitSettled(window time.Duration) error {
	if window <= 0 {
		return nil
	}
	deadline := time.Now().Add(window)
	for {
		if !s.Alive() {
			return s.enrichDeadError(fmt.Errorf("subprocess exited shortly after MCP handshake"))
		}
		if !time.Now().Before(deadline) {
			if !s.Alive() {
				return s.enrichDeadError(fmt.Errorf("subprocess exited shortly after MCP handshake"))
			}
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// readLoop 持续从 stdout 读取消息，按类型分发。
func (s *Session) readLoop() {
	for {
		msg, err := s.reader.Read()
		if err != nil {
			if err == io.EOF || s.closed.Load() {
				return
			}
			return
		}

		if msg.IsResponse() {
			s.pending.Dispatch(msg)
		}
		// 通知类消息（无 id）暂不处理——stdio 通道中的通知
		// 无法追溯到发起者，后续可扩展为回调
	}
}

// drainStderr 持续读取子进程 stderr 到环形缓冲。
func (s *Session) drainStderr() {
	_, _ = io.Copy(s.stderrBuf, s.proc.Stderr)
}

// waitProc 等待子进程退出并通知所有等待者。
func (s *Session) waitProc() {
	s.exitErr = s.proc.Wait()
	close(s.exitCh)

	// 子进程异常退出时上报诊断（既有通道；健康检查产品化另开）
	if !s.closed.Load() && s.cfg.DiagCallback != nil {
		diag := ClassifyExit(s.cfg.EndpointID, s.proc, s.handshakeErr, s.stderrBuf)
		s.cfg.DiagCallback(diag)
	}
}

func (s *Session) touchActivity() {
	s.lastActivity.Store(time.Now().UnixNano())
}
