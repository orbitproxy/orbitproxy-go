package wire

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/utils"
)

// ClientHelloCanonicalVersion is the signed canonical preamble.
const ClientHelloCanonicalVersion = "orbitproxy-edge-client-hello/v1"

// ClientHelloCanonicalString builds the string that is signed / verified.
// Identity line is client_key.
func ClientHelloCanonicalString(clientKey string, timestamp int64, nonce, softVersion string) string {
	lines := []string{
		ClientHelloCanonicalVersion,
		strings.TrimSpace(clientKey),
		strconv.FormatInt(timestamp, 10),
		strings.TrimSpace(nonce),
		strings.TrimSpace(softVersion),
	}
	return strings.Join(lines, "\n") + "\n"
}

// NewClientHello constructs an unsigned ClientHello with a fresh nonce.
func NewClientHello(clientKey, softVersion string) (ClientHello, error) {
	nonce, err := randomNonce()
	if err != nil {
		return ClientHello{}, err
	}
	return ClientHello{
		ClientKey:   strings.TrimSpace(clientKey),
		Timestamp:   time.Now().Unix(),
		Nonce:       nonce,
		SoftVersion: strings.TrimSpace(softVersion),
	}, nil
}

// SignClientHello signs hello with a PEM-encoded PKCS#8 ed25519 private key.
func SignClientHello(privateKeyPEM string, hello ClientHello) (ClientHello, error) {
	privateKey, err := utils.ParseEd25519PrivateKey(privateKeyPEM)
	if err != nil {
		return ClientHello{}, err
	}
	canonical := ClientHelloCanonicalString(
		hello.ClientKey,
		hello.Timestamp,
		hello.Nonce,
		hello.SoftVersion,
	)
	signature := ed25519.Sign(privateKey, []byte(canonical))
	hello.AuthSignature = base64.StdEncoding.EncodeToString(signature)
	return hello, nil
}

// VerifyClientHelloSignature verifies auth_signature against publicKeyPEM.
func VerifyClientHelloSignature(publicKeyPEM, signatureB64, canonical string) error {
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signatureB64))
	if err != nil {
		return fmt.Errorf("auth_signature is not valid base64")
	}
	publicKey, err := utils.ParseEd25519PublicKey(publicKeyPEM)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, []byte(canonical), signature) {
		return fmt.Errorf("auth_signature verification failed")
	}
	return nil
}

func randomNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate client hello nonce: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
