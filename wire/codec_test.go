package wire_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/orbitproxy/orbitproxy-go/wire"
)

func TestMsgWireRoundTrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	original := wire.ClientHello{
		ClientKey:   "ck_test",
		SoftVersion: "1.0.0",
		Timestamp:   1748245678,
		Nonce:       "nonce-1",
	}
	if err := wire.WriteMsg(&buf, original); err != nil {
		t.Fatalf("WriteMsg: %v", err)
	}
	if buf.Bytes()[0] != wire.TypeClientHello {
		t.Fatalf("type byte = %q, want %q", buf.Bytes()[0], wire.TypeClientHello)
	}

	decoded, err := wire.ReadMsg(&buf)
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	hello, ok := decoded.(*wire.ClientHello)
	if !ok {
		t.Fatalf("decoded type = %T, want *wire.ClientHello", decoded)
	}
	if hello.ClientKey != "ck_test" {
		t.Fatalf("ClientKey = %q", hello.ClientKey)
	}
}

func TestEmptyMessageWireSize(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := wire.WriteMsg(&buf, wire.ReqWorkConn{}); err != nil {
		t.Fatalf("WriteMsg: %v", err)
	}
	if buf.Len() != 1+8+2 {
		t.Fatalf("encoded len = %d, want 11", buf.Len())
	}
}

func TestNewEndpointRoundTrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	original := wire.NewEndpoint{
		EndpointID:          "ep-1",
		ProxyID:             "px-1",
		ProxyType:           "basic",
		Protocol:            "https",
		LocalServicePayload: json.RawMessage(`{"localAddr":"127.0.0.1:8080"}`),
	}
	if err := wire.WriteMsg(&buf, original); err != nil {
		t.Fatalf("WriteMsg: %v", err)
	}
	decoded, err := wire.ReadMsg(&buf)
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	got, ok := decoded.(*wire.NewEndpoint)
	if !ok || got.EndpointID != "ep-1" {
		t.Fatalf("decoded = %+v, ok=%v", decoded, ok)
	}
}

func TestDiscoverToolsRoundTrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	original := wire.DiscoverTools{
		RequestID:  "mer_1",
		EndpointID: "ep_1",
	}
	if err := wire.WriteMsg(&buf, original); err != nil {
		t.Fatalf("WriteMsg: %v", err)
	}
	if buf.Bytes()[0] != wire.TypeDiscoverTools {
		t.Fatalf("type byte = %q, want %q", buf.Bytes()[0], wire.TypeDiscoverTools)
	}
	decoded, err := wire.ReadMsg(&buf)
	if err != nil {
		t.Fatalf("ReadMsg: %v", err)
	}
	got, ok := decoded.(*wire.DiscoverTools)
	if !ok || got.RequestID != "mer_1" || got.EndpointID != "ep_1" {
		t.Fatalf("decoded = %+v, ok=%v", decoded, ok)
	}

	var out bytes.Buffer
	resp := wire.DiscoverToolsResult{
		RequestID:  "mer_1",
		EndpointID: "ep_1",
		Status:     "succeeded",
		Tools:      []wire.DiscoveredTool{{Name: "echo"}},
	}
	if err := wire.WriteMsg(&out, resp); err != nil {
		t.Fatalf("WriteMsg response: %v", err)
	}
	if out.Bytes()[0] != wire.TypeDiscoverToolsResult {
		t.Fatalf("response type byte = %q", out.Bytes()[0])
	}

	var epBuf bytes.Buffer
	ep := wire.NewEndpoint{
		EndpointID: "ep_1",
		ProxyID:    "px_1",
		DiscoverTools: &wire.DiscoverToolsOptions{
			RequestID: "mer_2",
		},
	}
	if err := wire.WriteMsg(&epBuf, ep); err != nil {
		t.Fatalf("WriteMsg NewEndpoint: %v", err)
	}
	epDecoded, err := wire.ReadMsg(&epBuf)
	if err != nil {
		t.Fatalf("ReadMsg NewEndpoint: %v", err)
	}
	epGot := epDecoded.(*wire.NewEndpoint)
	if epGot.DiscoverTools == nil || epGot.DiscoverTools.RequestID != "mer_2" {
		t.Fatalf("DiscoverTools = %+v", epGot.DiscoverTools)
	}
}

func TestReqWorkConnLengthPrefix(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := wire.WriteMsg(&buf, wire.ReqWorkConn{}); err != nil {
		t.Fatalf("WriteMsg: %v", err)
	}
	var length int64
	if err := binary.Read(bytes.NewReader(buf.Bytes()[1:9]), binary.BigEndian, &length); err != nil {
		t.Fatalf("read length: %v", err)
	}
	if length != 2 {
		t.Fatalf("payload length = %d, want 2", length)
	}
}
