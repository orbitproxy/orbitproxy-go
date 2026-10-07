package wire

import (
	"crypto/ed25519"
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/orbitproxy/orbitproxy-go/internal/utils"
)

// DataHelloCanonicalVersion is the signed preamble. It is not interchangeable
// with ClientHelloCanonicalVersion.
const DataHelloCanonicalVersion = "orbitproxy-edge-data-hello/v1"

// DataHelloCanonicalString builds the signed DataHello string.
// Order: session_id, machine_key, index, timestamp, nonce.
// String fields use TrimSpace, matching ClientHello, so both sides sign the same bytes.
func DataHelloCanonicalString(sessionID, machineKey string, index int, timestamp int64, nonce string) string {
	lines := []string{
		DataHelloCanonicalVersion,
		strings.TrimSpace(sessionID),
		strings.TrimSpace(machineKey),
		strconv.Itoa(index),
		strconv.FormatInt(timestamp, 10),
		strings.TrimSpace(nonce),
	}
	return strings.Join(lines, "\n") + "\n"
}

// SignDataHello signs hello with a PEM-encoded PKCS#8 ed25519 private key.
func SignDataHello(privateKeyPEM string, hello DataHello) (DataHello, error) {
	privateKey, err := utils.ParseEd25519PrivateKey(privateKeyPEM)
	if err != nil {
		return DataHello{}, err
	}
	canonical := DataHelloCanonicalString(
		hello.SessionID,
		hello.MachineKey,
		hello.Index,
		hello.Timestamp,
		hello.Nonce,
	)
	signature := ed25519.Sign(privateKey, []byte(canonical))
	hello.AuthSignature = base64.StdEncoding.EncodeToString(signature)
	return hello, nil
}
