package mcpstdio

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// Message 表示一条 JSON-RPC 2.0 消息（请求、响应或通知）。
// 字段均为 json.RawMessage 以避免反序列化开销——bridge 只关心 id 和 method。
type Message struct {
	JSONRPC json.RawMessage `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  json.RawMessage `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// IsRequest 判断消息是否为请求（有 id 且有 method）。
func (m *Message) IsRequest() bool {
	return len(m.ID) > 0 && len(m.Method) > 0
}

// IsResponse 判断消息是否为响应（有 id 且无 method）。
func (m *Message) IsResponse() bool {
	return len(m.ID) > 0 && len(m.Method) == 0
}

// IsNotification 判断消息是否为通知（无 id 且有 method）。
func (m *Message) IsNotification() bool {
	return len(m.ID) == 0 && len(m.Method) > 0
}

// MethodString 返回 method 字段的纯字符串值。
func (m *Message) MethodString() string {
	if len(m.Method) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(m.Method, &s); err != nil {
		return ""
	}
	return s
}

// ----------------------------------------------------------------
// Writer：向子进程 stdin 写入行分隔 JSON-RPC
// ----------------------------------------------------------------

// Writer 向 io.Writer 写入行分隔的 JSON-RPC 消息。
// 线程安全——多个 goroutine 可并发调用 Write。
type Writer struct {
	mu sync.Mutex
	w  io.Writer
}

// NewWriter 创建一个面向 stdin 管道的 JSON-RPC writer。
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// Write 将消息序列化为一行 JSON + '\n' 写入底层 writer。
func (w *Writer) Write(msg *Message) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal jsonrpc: %w", err)
	}
	raw = append(raw, '\n')

	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.w.Write(raw)
	return err
}

// ----------------------------------------------------------------
// Reader：从子进程 stdout 按行读取 JSON-RPC
// ----------------------------------------------------------------

// Reader 从 io.Reader 按行读取 JSON-RPC 消息。
// 每行是一条完整的 JSON-RPC 对象。
type Reader struct {
	scanner *bufio.Scanner
}

// NewReader 创建一个面向 stdout 管道的 JSON-RPC reader。
func NewReader(r io.Reader) *Reader {
	scanner := bufio.NewScanner(r)
	// 单行 JSON-RPC 消息通常不大，但工具返回体可能较大；设上限 4MB。
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	return &Reader{scanner: scanner}
}

// Read 阻塞读取下一条 JSON-RPC 消息。
// 返回 io.EOF 表示管道关闭。
func (r *Reader) Read() (*Message, error) {
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	line := r.scanner.Bytes()
	if len(line) == 0 {
		return r.Read()
	}
	msg := &Message{}
	if err := json.Unmarshal(line, msg); err != nil {
		return nil, fmt.Errorf("decode jsonrpc line: %w", err)
	}
	return msg, nil
}

// ----------------------------------------------------------------
// PendingMap：按 id 分发响应到对应的等待者
// ----------------------------------------------------------------

// pendingEntry 是一个正在等待响应的请求。
type pendingEntry struct {
	ch         chan *Message
	originalID json.RawMessage // 客户端原始 id，用于响应时还原
	deadline   time.Time
}

// PendingMap 管理等待中的 JSON-RPC 请求，按 id 关联响应。
// stdio 管道是单通道——所有请求共享 stdin、所有响应共享 stdout，
// 必须靠 id 做多路复用。
type PendingMap struct {
	mu      sync.Mutex
	entries map[string]*pendingEntry
	nextID  atomic.Int64 // 单调递增的内部 id 生成器
}

// NewPendingMap 创建请求响应关联表。
func NewPendingMap() *PendingMap {
	pm := &PendingMap{
		entries: make(map[string]*pendingEntry),
	}
	pm.nextID.Store(1)
	return pm
}

// Register 为一个出站请求分配内部 id，注册等待通道，返回重写后的 id。
// originalID 是客户端的原始 id，会在收到响应后还原。
// timeout 是请求级超时，超时后 entry 自动清理。
func (pm *PendingMap) Register(originalID json.RawMessage, timeout time.Duration) (internalID json.RawMessage, wait <-chan *Message) {
	id := pm.nextID.Add(1)
	idBytes, _ := json.Marshal(id)
	idKey := string(idBytes)

	ch := make(chan *Message, 1)
	entry := &pendingEntry{
		ch:         ch,
		originalID: originalID,
		deadline:   time.Now().Add(timeout),
	}

	pm.mu.Lock()
	pm.entries[idKey] = entry
	pm.mu.Unlock()

	// 超时后自动清理，避免子进程吞掉请求后 chan 永远阻塞
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		<-timer.C

		pm.mu.Lock()
		if e, ok := pm.entries[idKey]; ok && e == entry {
			delete(pm.entries, idKey)
			close(ch)
		}
		pm.mu.Unlock()
	}()

	return idBytes, ch
}

// Dispatch 将一条来自 stdout 的响应按 id 分发给等待者。
// 分发前会将 id 还原为客户端原始值。
// 如果找不到等待者（已超时或 id 不匹配），返回 false。
func (pm *PendingMap) Dispatch(msg *Message) bool {
	if !msg.IsResponse() {
		return false
	}
	idKey := string(msg.ID)

	pm.mu.Lock()
	entry, ok := pm.entries[idKey]
	if ok {
		delete(pm.entries, idKey)
	}
	pm.mu.Unlock()

	if !ok {
		return false
	}

	// 还原客户端原始 id
	msg.ID = entry.originalID
	entry.ch <- msg
	return true
}

// CloseAll 关闭所有等待中的 chan，用于会话关闭时避免泄漏。
func (pm *PendingMap) CloseAll() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for key, entry := range pm.entries {
		close(entry.ch)
		delete(pm.entries, key)
	}
}

// Len 返回当前等待中的请求数。
func (pm *PendingMap) Len() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return len(pm.entries)
}
