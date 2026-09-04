package mcpstdio

import (
	"testing"
	"time"
)

func TestStderrBuffer(t *testing.T) {
	sb := NewStderrBuffer()

	// 写入少量数据
	sb.Write([]byte("hello\n"))
	tail := sb.Tail()
	if string(tail) != "hello\n" {
		t.Errorf("tail = %q, want %q", tail, "hello\n")
	}
}

func TestStderrBufferOverflow(t *testing.T) {
	sb := NewStderrBuffer()

	// 写入超过 4KB 的数据，应只保留尾部
	data := make([]byte, stderrBufSize+100)
	for i := range data {
		data[i] = byte('A' + (i % 26))
	}
	sb.Write(data)

	tail := sb.Tail()
	if len(tail) != stderrBufSize {
		t.Errorf("tail length = %d, want %d", len(tail), stderrBufSize)
	}
	// 尾部应等于原始数据的最后 4KB
	expected := data[len(data)-stderrBufSize:]
	for i, b := range tail {
		if b != expected[i] {
			t.Errorf("tail[%d] = %c, want %c", i, b, expected[i])
			break
		}
	}
}

func TestStderrBufferMultipleWrites(t *testing.T) {
	sb := NewStderrBuffer()

	// 多次小写入，总量超过 4KB
	chunk := make([]byte, 1024)
	for i := range chunk {
		chunk[i] = byte('0' + (i % 10))
	}
	for i := 0; i < 10; i++ {
		sb.Write(chunk)
	}

	tail := sb.Tail()
	if len(tail) != stderrBufSize {
		t.Errorf("tail length = %d, want %d", len(tail), stderrBufSize)
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string // 验证输出不包含原始凭证
	}{
		{
			name:     "url with password",
			input:    "connecting to postgres://admin:s3cret@db.host:5432/mydb",
			contains: "***",
		},
		{
			name:     "bearer token",
			input:    "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig",
			contains: "[REDACTED]",
		},
		{
			name:     "openai api key",
			input:    "Error: sk-proj-abcdefg12345678901234567890",
			contains: "[REDACTED]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := string(Sanitize([]byte(tt.input)))
			if result == tt.input {
				t.Error("sanitize did not modify the input")
			}
		})
	}
}

func TestSanitizePassthrough(t *testing.T) {
	// 普通文本不应被修改
	input := "Error: connection refused at localhost:3000"
	result := string(Sanitize([]byte(input)))
	if result != input {
		t.Errorf("sanitize modified normal text: %q -> %q", input, result)
	}
}

func TestDeduplicator(t *testing.T) {
	dedup := NewDeduplicator(500 * time.Millisecond)

	diag := Diagnostic{
		EndpointID: "ep-1",
		Code:       CodeExitedOnStart,
		Message:    "process crashed",
	}

	// 首次应该上报
	_, shouldReport := dedup.Record(diag)
	if !shouldReport {
		t.Error("first occurrence should be reported")
	}

	// 窗口内重复不应上报
	merged, shouldReport := dedup.Record(diag)
	if shouldReport {
		t.Error("duplicate within window should not report")
	}
	if merged.OccurCount != 2 {
		t.Errorf("OccurCount = %d, want 2", merged.OccurCount)
	}

	// 等窗口过期后应重新上报
	time.Sleep(600 * time.Millisecond)
	_, shouldReport = dedup.Record(diag)
	if !shouldReport {
		t.Error("after window expiry should report again")
	}
}
