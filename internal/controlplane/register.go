package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

const publicKeyAlgorithm = "ed25519"

// RegisterResult is the successful machine restore response.
// Only edge addr is returned; identity MachineKey is the caller's input.
type RegisterResult struct {
	EdgeAddr string
	CACert   string
}

// RegisterRequest is the POST /v1/machines/register JSON body.
// SDK always sends MachineKey (restore). CLI has its own register client.
type RegisterRequest struct {
	AuthToken          string `json:"authtoken"`
	MachineKey          string `json:"machineKey"`
	Hostname           string `json:"hostname,omitempty"`
	PublicKeyAlgorithm string `json:"publicKeyAlgorithm"`
	PublicKey          string `json:"publicKey"`
	OS                 string `json:"os,omitempty"`
	Arch               string `json:"arch,omitempty"`
	Version            string `json:"version,omitempty"`
}

type registerResponse struct {
	Edge struct {
		Addr   string `json:"addr"`
		CACert string `json:"caCert"`
	} `json:"edge"`
	// EdgeAddr accepts a flat {"edge_addr":"..."} shape as well.
	EdgeAddr string `json:"edge_addr"`
	CACert   string `json:"caCert"`
}

type apiEnvelope struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    registerResponse `json:"data"`
}

// RegisterOptions configures the machine restore call (SDK path).
type RegisterOptions struct {
	APIURL     string
	AuthToken  string
	MachineKey  string
	PublicKey  string
	Hostname   string
	Version    string
	HTTPClient *http.Client
}

// Register restores the machine mapped by MachineKey using
// /v1/machines/register. Response only needs edge.addr.
func Register(ctx context.Context, opts RegisterOptions) (*RegisterResult, error) {
	apiURL := strings.TrimSpace(opts.APIURL)
	if apiURL == "" {
		return nil, fmt.Errorf("APIURL is required")
	}
	authToken := strings.TrimSpace(opts.AuthToken)
	if authToken == "" {
		return nil, fmt.Errorf("AuthToken is required")
	}
	machineKey := strings.TrimSpace(opts.MachineKey)
	if machineKey == "" {
		return nil, fmt.Errorf("MachineKey is required")
	}
	publicKey := strings.TrimSpace(opts.PublicKey)
	if publicKey == "" {
		return nil, fmt.Errorf("public key is required")
	}

	base, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("parse APIURL: %w", err)
	}
	endpoint := base.JoinPath("/v1/machines/register")

	body := RegisterRequest{
		AuthToken:          authToken,
		MachineKey:          machineKey,
		Hostname:           strings.TrimSpace(opts.Hostname),
		PublicKeyAlgorithm: publicKeyAlgorithm,
		PublicKey:          publicKey,
		OS:                 runtime.GOOS,
		Arch:               runtime.GOARCH,
		Version:            strings.TrimSpace(opts.Version),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal register request: %w", err)
	}

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build register request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("register request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read register response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("register failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	data, err := decodeRegisterResponse(raw)
	if err != nil {
		return nil, err
	}
	edgeAddr := strings.TrimSpace(data.Edge.Addr)
	if edgeAddr == "" {
		edgeAddr = strings.TrimSpace(data.EdgeAddr)
	}
	if edgeAddr == "" {
		return nil, fmt.Errorf("register response missing edge.addr")
	}

	caCert := strings.TrimSpace(data.Edge.CACert)
	if caCert == "" {
		caCert = strings.TrimSpace(data.CACert)
	}
	if caCert == "" {
		return nil, fmt.Errorf("register response missing edge.caCert")
	}
	return &RegisterResult{EdgeAddr: edgeAddr, CACert: caCert}, nil
}

func decodeRegisterResponse(raw []byte) (*registerResponse, error) {
	var env apiEnvelope
	if err := json.Unmarshal(raw, &env); err == nil && (env.Data.Edge.Addr != "" || env.Data.EdgeAddr != "") {
		if env.Code != 0 && env.Code != 200 {
			msg := env.Message
			if msg == "" {
				msg = "register failed"
			}
			return nil, fmt.Errorf("%s", msg)
		}
		return &env.Data, nil
	}

	var direct registerResponse
	if err := json.Unmarshal(raw, &direct); err != nil {
		return nil, fmt.Errorf("decode register response: %w", err)
	}
	return &direct, nil
}
