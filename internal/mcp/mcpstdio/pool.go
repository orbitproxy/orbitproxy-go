package mcpstdio

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// PoolConfig 控制会话池的行为参数。
type PoolConfig struct {
	// MaxSessionsPerEndpoint 单个 endpoint 最大并发会话数，默认 8
	MaxSessionsPerEndpoint int
	// MaxGlobalProcesses Machine 全局最大子进程数，默认 32
	MaxGlobalProcesses int
	// IdleTimeout 会话空闲超时，超时后回收子进程，默认 300s
	IdleTimeout time.Duration
	// PIDFile PID 记录文件路径
	PIDFile string
	// MachineDir 是 <workdir>/<machineKey>，spawn 时读 env/<endpointID>.env
	MachineDir string
	// DiagCallback 诊断事件回调
	DiagCallback DiagnosticCallback
	// Logger 日志
	Logger *slog.Logger
}

// DefaultPoolConfig 返回默认池配置。
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxSessionsPerEndpoint: 8,
		MaxGlobalProcesses:     32,
		IdleTimeout:            300 * time.Second,
	}
}

type createFlight struct {
	done    chan struct{}
	session *Session
	err     error
}

// Pool 管理所有 exec 模式 endpoint 的会话。
// 职责：
//   - 按 key 获取/创建会话（singleflight，握手期间不重复 spawn）
//   - 空闲回收
//   - 并发上限控制
//   - 配置变更时重建
type Pool struct {
	mu          sync.Mutex
	sessions    map[string]*Session      // key → session
	creating    map[string]*createFlight // key → in-flight create
	epCount     map[string]int           // endpointID → 该 endpoint 下的会话数
	configs     map[string]SessionConfig // endpointID → 最新配置
	globalCount atomic.Int32

	cfg     PoolConfig
	stopCh  chan struct{}
	stopped atomic.Bool
}

// NewPool 创建会话池并启动空闲回收协程。
func NewPool(cfg PoolConfig) *Pool {
	if cfg.MaxSessionsPerEndpoint <= 0 {
		cfg.MaxSessionsPerEndpoint = 8
	}
	if cfg.MaxGlobalProcesses <= 0 {
		cfg.MaxGlobalProcesses = 32
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 300 * time.Second
	}

	p := &Pool{
		sessions: make(map[string]*Session),
		creating: make(map[string]*createFlight),
		epCount:  make(map[string]int),
		configs:  make(map[string]SessionConfig),
		cfg:      cfg,
		stopCh:   make(chan struct{}),
	}
	go p.idleReaper()
	return p
}

// RegisterEndpoint 注册或更新一个 endpoint 的 exec 配置。
// 配置变更（command/args/env 不同）会关闭该 endpoint 的所有现有会话。
func (p *Pool) RegisterEndpoint(endpointID string, cfg SessionConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()

	old, exists := p.configs[endpointID]
	p.configs[endpointID] = cfg

	// 配置变更时关闭所有该 endpoint 的会话（command/args/env 改了必须用新进程）
	if exists && configChanged(old.SpawnConfig, cfg.SpawnConfig) {
		p.closeEndpointSessionsLocked(endpointID)
	}
}

// UnregisterEndpoint 移除一个 endpoint 的配置并关闭其所有会话。
func (p *Pool) UnregisterEndpoint(endpointID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.configs, endpointID)
	p.closeEndpointSessionsLocked(endpointID)
}

// GetOrCreate 获取已有会话或创建新会话。
// sessionKey 用于区分不同 AI 客户端的会话（派生自请求头）。
// endpointID 用于查找配置和做并发限额。
// 同一 sessionKey 的并发创建会 singleflight：握手期间排队等待，不重复 spawn。
func (p *Pool) GetOrCreate(sessionKey string, endpointID string) (*Session, error) {
	for {
		p.mu.Lock()

		if p.stopped.Load() {
			p.mu.Unlock()
			return nil, fmt.Errorf("session pool stopped")
		}

		// 命中已有会话
		if s, ok := p.sessions[sessionKey]; ok {
			if s.Alive() {
				p.mu.Unlock()
				return s, nil
			}
			// 会话已死，清理
			p.removeSessionLocked(sessionKey, endpointID)
		}

		// 已有创建中：等待结果
		if flight, ok := p.creating[sessionKey]; ok {
			p.mu.Unlock()
			<-flight.done
			if flight.err != nil {
				return nil, flight.err
			}
			if flight.session != nil && flight.session.Alive() {
				return flight.session, nil
			}
			// 创建结果已死或被踢掉，重试
			continue
		}

		// 检查并发限额
		if int(p.globalCount.Load()) >= p.cfg.MaxGlobalProcesses {
			p.mu.Unlock()
			return nil, fmt.Errorf("global process limit reached (%d)", p.cfg.MaxGlobalProcesses)
		}
		if p.epCount[endpointID] >= p.cfg.MaxSessionsPerEndpoint {
			p.mu.Unlock()
			return nil, fmt.Errorf("endpoint session limit reached (%d)", p.cfg.MaxSessionsPerEndpoint)
		}

		cfg, ok := p.configs[endpointID]
		if !ok {
			p.mu.Unlock()
			return nil, fmt.Errorf("endpoint %s not registered", endpointID)
		}

		flight := &createFlight{done: make(chan struct{})}
		p.creating[sessionKey] = flight
		// 提前占位计数，避免并发创建超限
		p.epCount[endpointID]++
		p.globalCount.Add(1)
		p.mu.Unlock()

		// 创建会话（可能耗时：spawn + handshake + settle）
		cfg.PIDFile = p.cfg.PIDFile
		cfg.DiagCallback = p.cfg.DiagCallback
		cfg.EndpointID = endpointID
		if cfg.MachineDir == "" {
			cfg.MachineDir = p.cfg.MachineDir
		}
		session, err := NewSession(cfg)

		p.mu.Lock()
		delete(p.creating, sessionKey)
		if err != nil {
			p.epCount[endpointID]--
			if p.epCount[endpointID] == 0 {
				delete(p.epCount, endpointID)
			}
			p.globalCount.Add(-1)
			flight.err = err
			close(flight.done)
			p.mu.Unlock()
			return nil, err
		}

		p.sessions[sessionKey] = session
		flight.session = session
		close(flight.done)
		p.mu.Unlock()

		p.log().Debug("session created",
			"session_key", sessionKey,
			"endpoint_id", endpointID,
			"pid", session.proc.PID(),
		)

		return session, nil
	}
}

// Invalidate 丢弃已死或出错的会话，下次 GetOrCreate 会重新 spawn。
func (p *Pool) Invalidate(sessionKey string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.sessions[sessionKey]
	if !ok || s == nil {
		return
	}
	p.removeSessionLocked(sessionKey, s.cfg.EndpointID)
}

// CloseAll 优雅关闭所有会话。Machine 退出时调用。
func (p *Pool) CloseAll() {
	p.stopped.Store(true)
	close(p.stopCh)

	p.mu.Lock()
	keys := make([]string, 0, len(p.sessions))
	for k := range p.sessions {
		keys = append(keys, k)
	}
	p.mu.Unlock()

	for _, key := range keys {
		p.mu.Lock()
		s, ok := p.sessions[key]
		if ok {
			delete(p.sessions, key)
		}
		p.mu.Unlock()
		if ok && s != nil {
			_ = s.Close()
			p.globalCount.Add(-1)
		}
	}

	p.mu.Lock()
	p.epCount = make(map[string]int)
	p.mu.Unlock()
}

// SessionCount 返回当前活跃会话数。
func (p *Pool) SessionCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sessions)
}

// ----------------------------------------------------------------
// 内部方法
// ----------------------------------------------------------------

// idleReaper 定期扫描并关闭空闲超时的会话。
// 扫描周期为 IdleTimeout/2，确保最大延迟不超过 1.5 倍超时。
func (p *Pool) idleReaper() {
	interval := p.cfg.IdleTimeout / 2
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.reapIdle()
		}
	}
}

func (p *Pool) reapIdle() {
	now := time.Now()
	var toClose []string

	p.mu.Lock()
	for key, s := range p.sessions {
		if !s.Alive() || now.Sub(s.LastActivity()) > p.cfg.IdleTimeout {
			toClose = append(toClose, key)
		}
	}
	p.mu.Unlock()

	for _, key := range toClose {
		p.mu.Lock()
		s, ok := p.sessions[key]
		if !ok {
			p.mu.Unlock()
			continue
		}
		// 找到该 session 对应的 endpointID
		endpointID := s.cfg.EndpointID
		delete(p.sessions, key)
		p.epCount[endpointID]--
		if p.epCount[endpointID] <= 0 {
			delete(p.epCount, endpointID)
		}
		p.globalCount.Add(-1)
		p.mu.Unlock()

		p.log().Debug("session idle reaped",
			"session_key", key,
			"endpoint_id", endpointID,
			"idle_seconds", now.Sub(s.LastActivity()).Seconds(),
		)
		_ = s.Close()
	}
}

func (p *Pool) closeEndpointSessionsLocked(endpointID string) {
	var toClose []*Session
	for key, s := range p.sessions {
		if s.cfg.EndpointID == endpointID {
			toClose = append(toClose, s)
			delete(p.sessions, key)
		}
	}
	delete(p.epCount, endpointID)

	// 在锁外关闭（Close 可能阻塞）
	go func() {
		for _, s := range toClose {
			_ = s.Close()
			p.globalCount.Add(-1)
		}
	}()
}

func (p *Pool) removeSessionLocked(sessionKey, endpointID string) {
	if s, ok := p.sessions[sessionKey]; ok {
		delete(p.sessions, sessionKey)
		p.epCount[endpointID]--
		if p.epCount[endpointID] <= 0 {
			delete(p.epCount, endpointID)
		}
		p.globalCount.Add(-1)
		go func() { _ = s.Close() }()
	}
}

func (p *Pool) log() *slog.Logger {
	if p.cfg.Logger != nil {
		return p.cfg.Logger
	}
	return slog.Default()
}

// configChanged 判断两个 SpawnConfig 是否有实质变化（需要重建会话）。
func configChanged(a, b SpawnConfig) bool {
	if a.Command != b.Command {
		return true
	}
	if a.WorkDir != b.WorkDir {
		return true
	}
	if len(a.Args) != len(b.Args) {
		return true
	}
	for i := range a.Args {
		if a.Args[i] != b.Args[i] {
			return true
		}
	}
	if len(a.Env) != len(b.Env) {
		return true
	}
	for i := range a.Env {
		if a.Env[i] != b.Env[i] {
			return true
		}
	}
	return false
}
