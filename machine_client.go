package orbitproxy

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const machineSignatureVersion = "OP-MACHINE-V1"

// MachineClient 调用本期新增的机器接口。它不连接边缘，也不创建隧道。
type MachineClient struct {
	APIURL     string
	MachineKey string
	PrivateKey ed25519.PrivateKey
	HTTP       *http.Client
	Now        func() time.Time
}

type MachineClientOptions struct {
	APIURL        string
	MachineKey    string
	PrivateKeyPEM string
	HTTPClient    *http.Client
}

type MachineAPIError struct {
	Status  int
	Code    string
	Message string
}

func (err *MachineAPIError) Error() string {
	if err.Message != "" {
		return err.Message
	}
	return fmt.Sprintf("control-plane returned %d %s", err.Status, err.Code)
}

func NewMachineClient(opts MachineClientOptions) (*MachineClient, error) {
	block, _ := pem.Decode([]byte(opts.PrivateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("parse machine private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("machine private key is not ed25519")
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &MachineClient{
		APIURL:     strings.TrimRight(opts.APIURL, "/"),
		MachineKey: opts.MachineKey,
		PrivateKey: key,
		HTTP:       httpClient,
	}, nil
}

func (client *MachineClient) ApplyPolicy(ctx context.Context, proxyID string, rule any) (json.RawMessage, error) {
	return client.do(ctx, http.MethodPost, "/v1/machine-api/tunnels/"+url.PathEscape(proxyID)+"/policies", rule)
}

func (client *MachineClient) ListPolicies(ctx context.Context, proxyID string) (json.RawMessage, error) {
	return client.do(ctx, http.MethodGet, "/v1/machine-api/tunnels/"+url.PathEscape(proxyID)+"/policies", nil)
}

func (client *MachineClient) DeletePolicy(ctx context.Context, proxyID, policyType, ruleGroupID string) error {
	_, err := client.do(ctx, http.MethodDelete, "/v1/machine-api/tunnels/"+url.PathEscape(proxyID)+"/policies/"+url.PathEscape(policyType)+"/"+url.PathEscape(ruleGroupID), nil)
	return err
}

func (client *MachineClient) SetMaintenance(ctx context.Context, proxyID string, enabled bool) (json.RawMessage, error) {
	return client.do(ctx, http.MethodPost, "/v1/machine-api/tunnels/"+url.PathEscape(proxyID)+"/maintenance", map[string]bool{"enabled": enabled})
}

func (client *MachineClient) ListAccessLogs(ctx context.Context, proxyID string) (json.RawMessage, error) {
	return client.do(ctx, http.MethodGet, "/v1/machine-api/tunnels/"+url.PathEscape(proxyID)+"/access-logs", nil)
}

func (client *MachineClient) do(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = encoded
	}
	nonce, err := randomNonce()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if client.Now != nil {
		now = client.Now()
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	signature := signMachine(client.PrivateKey, method, path, nil, payload, timestamp, nonce, client.MachineKey)
	request, err := http.NewRequestWithContext(ctx, method, client.APIURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-OrbitProxy-Machine-Key", client.MachineKey)
	request.Header.Set("X-OrbitProxy-Timestamp", timestamp)
	request.Header.Set("X-OrbitProxy-Nonce", nonce)
	request.Header.Set("X-OrbitProxy-Signature", signature)
	response, err := client.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusNotFound && !json.Valid(raw) {
		return nil, &MachineAPIError{Status: response.StatusCode, Code: "server_unsupported", Message: "control plane does not support this command"}
	}
	var envelope struct {
		Error   string          `json:"error"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		if response.StatusCode == http.StatusNotFound {
			return nil, &MachineAPIError{Status: response.StatusCode, Code: "server_unsupported", Message: "control plane does not support this command"}
		}
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || envelope.Error != "" {
		return nil, &MachineAPIError{Status: response.StatusCode, Code: envelope.Error, Message: envelope.Message}
	}
	return envelope.Data, nil
}

func signMachine(privateKey ed25519.PrivateKey, method, path string, query url.Values, body []byte, timestamp, nonce, machineKey string) string {
	sum := sha256.Sum256(body)
	pairs := make([]string, 0)
	for key, values := range query {
		for _, value := range values {
			pairs = append(pairs, key+"="+value)
		}
	}
	sort.Strings(pairs)
	canonical := strings.Join([]string{
		machineSignatureVersion,
		method,
		path,
		strings.Join(pairs, "&"),
		hex.EncodeToString(sum[:]),
		timestamp,
		nonce,
		machineKey,
	}, "\n")
	return base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(canonical)))
}

func randomNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
