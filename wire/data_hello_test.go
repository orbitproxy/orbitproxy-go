package wire

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"reflect"
	"testing"
)

// 与 edge 侧测试使用同一条规范串，不在各自测试里临时拼接。
const dataHelloCanonicalVector = "orbitproxy-edge-data-hello/v1\nsess_test\nck_test\n2\n1748245678\nnonce-abc\n"

func TestServerHelloDataSessionsJSON(t *testing.T) {
	var missing ServerHello
	if err := json.Unmarshal([]byte(`{"edge_id":"e","session_id":"s"}`), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.DataSessions != 0 {
		t.Fatalf("missing data_sessions = %d", missing.DataSessions)
	}

	raw, err := json.Marshal(ServerHello{EdgeID: "e", SessionID: "s", DataSessions: 4})
	if err != nil {
		t.Fatal(err)
	}
	var decoded ServerHello
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.DataSessions != 4 {
		t.Fatalf("data_sessions = %d", decoded.DataSessions)
	}
}

func TestDataHelloCanonicalVector(t *testing.T) {
	got := DataHelloCanonicalString("sess_test", "ck_test", 2, 1748245678, "nonce-abc")
	if got != dataHelloCanonicalVector {
		t.Fatalf("canonical\n got %q\nwant %q", got, dataHelloCanonicalVector)
	}
	if !bytes.Contains([]byte(got), []byte("\n2\n")) {
		t.Fatal("canonical missing index")
	}
}

func TestDataHelloIndexIsSigned(t *testing.T) {
	privatePEM, publicPEM := testEd25519PEM(t)
	hello := DataHello{
		SessionID:  "sess_test",
		MachineKey: "ck_test",
		Index:      2,
		Timestamp:  1748245678,
		Nonce:      "nonce-abc",
	}
	signed, err := SignDataHello(privatePEM, hello)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyClientHelloSignature(publicPEM, signed.AuthSignature, dataHelloCanonicalVector); err != nil {
		t.Fatal(err)
	}
	changed := DataHelloCanonicalString(hello.SessionID, hello.MachineKey, 3, hello.Timestamp, hello.Nonce)
	if err := VerifyClientHelloSignature(publicPEM, signed.AuthSignature, changed); err == nil {
		t.Fatal("changed index still verified")
	}
	clientCanonical := ClientHelloCanonicalString("ck_test", 1748245678, "nonce-abc", "1.0.0")
	if err := VerifyClientHelloSignature(publicPEM, signed.AuthSignature, clientCanonical); err == nil {
		t.Fatal("client hello canonical accepted for data hello signature")
	}
}

func TestLegacyMapperRejectsDataHelloType(t *testing.T) {
	legacy := &MsgTransportMapper{
		typeMap:      map[byte]reflect.Type{},
		typeByteMap:  map[reflect.Type]byte{},
		maxMsgLength: defaultMaxMsgLength,
	}
	legacy.registerMsg(TypeServerHello, ServerHello{})

	var buf bytes.Buffer
	buf.WriteByte(TypeDataHello)
	if err := binary.Write(&buf, binary.BigEndian, int64(2)); err != nil {
		t.Fatal(err)
	}
	buf.WriteString("{}")
	_, err := legacy.Read(&buf)
	if !errors.Is(err, ErrMsgType) {
		t.Fatalf("err = %v", err)
	}
}

func testEd25519PEM(t *testing.T) (privatePEM, publicPEM string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}))
	publicPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
	return privatePEM, publicPEM
}
