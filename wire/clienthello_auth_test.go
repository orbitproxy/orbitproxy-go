package wire_test

import (
	"testing"

	"github.com/orbitproxy/orbitproxy-go/internal/utils"
	"github.com/orbitproxy/orbitproxy-go/wire"
)

func TestSignAndVerifyClientHello(t *testing.T) {
	t.Parallel()

	pub, priv, err := utils.GenerateEd25519KeyPairPEM()
	if err != nil {
		t.Fatalf("GenerateEd25519KeyPairPEM: %v", err)
	}

	hello, err := wire.NewClientHello("ck_1", "0.1.0")
	if err != nil {
		t.Fatalf("NewClientHello: %v", err)
	}
	signed, err := wire.SignClientHello(priv, hello)
	if err != nil {
		t.Fatalf("SignClientHello: %v", err)
	}
	if signed.AuthSignature == "" {
		t.Fatal("expected auth_signature")
	}

	canonical := wire.ClientHelloCanonicalString(
		signed.ClientKey, signed.Timestamp, signed.Nonce, signed.SoftVersion,
	)
	if err := wire.VerifyClientHelloSignature(pub, signed.AuthSignature, canonical); err != nil {
		t.Fatalf("VerifyClientHelloSignature: %v", err)
	}

	if err := wire.VerifyClientHelloSignature(pub, "AAAA", canonical); err == nil {
		t.Fatal("expected verification failure for bad signature")
	}
}
