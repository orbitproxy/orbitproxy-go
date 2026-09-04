package mcpstdio

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestWriterAndReader(t *testing.T) {
	// 用管道连接 Writer 和 Reader，模拟 stdin/stdout
	var buf bytes.Buffer
	w := NewWriter(&buf)
	r := NewReader(&buf)

	msg := &Message{
		JSONRPC: json.RawMessage(`"2.0"`),
		ID:      json.RawMessage(`1`),
		Method:  json.RawMessage(`"test/method"`),
		Params:  json.RawMessage(`{"key":"value"}`),
	}

	if err := w.Write(msg); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	got, err := r.Read()
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	if got.MethodString() != "test/method" {
		t.Errorf("method = %q, want %q", got.MethodString(), "test/method")
	}
	if !got.IsRequest() {
		t.Error("expected IsRequest=true")
	}
}

func TestReaderSkipsEmptyLines(t *testing.T) {
	input := "\n\n" + `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n\n"
	r := NewReader(strings.NewReader(input))

	msg, err := r.Read()
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if msg.MethodString() != "ping" {
		t.Errorf("method = %q, want %q", msg.MethodString(), "ping")
	}
}

func TestReaderEOF(t *testing.T) {
	r := NewReader(strings.NewReader(""))
	_, err := r.Read()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestMessageTypes(t *testing.T) {
	tests := []struct {
		name         string
		msg          Message
		isReq, isResp, isNotif bool
	}{
		{
			name:    "request",
			msg:     Message{ID: json.RawMessage(`1`), Method: json.RawMessage(`"foo"`)},
			isReq:   true,
		},
		{
			name:    "response with result",
			msg:     Message{ID: json.RawMessage(`1`), Result: json.RawMessage(`{}`)},
			isResp:  true,
		},
		{
			name:     "notification",
			msg:      Message{Method: json.RawMessage(`"event"`)},
			isNotif:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.msg.IsRequest() != tt.isReq {
				t.Errorf("IsRequest = %v, want %v", tt.msg.IsRequest(), tt.isReq)
			}
			if tt.msg.IsResponse() != tt.isResp {
				t.Errorf("IsResponse = %v, want %v", tt.msg.IsResponse(), tt.isResp)
			}
			if tt.msg.IsNotification() != tt.isNotif {
				t.Errorf("IsNotification = %v, want %v", tt.msg.IsNotification(), tt.isNotif)
			}
		})
	}
}

func TestPendingMapRegisterAndDispatch(t *testing.T) {
	pm := NewPendingMap()
	origID := json.RawMessage(`"client-123"`)

	internalID, wait := pm.Register(origID, 5*time.Second)

	// 模拟子进程返回响应
	resp := &Message{
		JSONRPC: json.RawMessage(`"2.0"`),
		ID:      internalID,
		Result:  json.RawMessage(`{"tools":[]}`),
	}

	if !pm.Dispatch(resp) {
		t.Fatal("dispatch should return true")
	}

	got := <-wait
	// 验证 id 被还原为原始值
	if string(got.ID) != string(origID) {
		t.Errorf("restored ID = %s, want %s", got.ID, origID)
	}
}

func TestPendingMapTimeout(t *testing.T) {
	pm := NewPendingMap()
	_, wait := pm.Register(json.RawMessage(`1`), 100*time.Millisecond)

	// 等待超时
	select {
	case _, ok := <-wait:
		if ok {
			t.Error("expected channel to be closed on timeout")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timeout waiting for channel close")
	}

	if pm.Len() != 0 {
		t.Errorf("pending map should be empty after timeout, got %d", pm.Len())
	}
}

func TestPendingMapCloseAll(t *testing.T) {
	pm := NewPendingMap()
	_, wait1 := pm.Register(json.RawMessage(`1`), 10*time.Second)
	_, wait2 := pm.Register(json.RawMessage(`2`), 10*time.Second)

	pm.CloseAll()

	// 两个 channel 都应被关闭
	select {
	case _, ok := <-wait1:
		if ok {
			t.Error("wait1 should be closed")
		}
	default:
		t.Error("wait1 should be readable (closed)")
	}

	select {
	case _, ok := <-wait2:
		if ok {
			t.Error("wait2 should be closed")
		}
	default:
		t.Error("wait2 should be readable (closed)")
	}
}
