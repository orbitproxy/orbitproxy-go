package endpoint

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"log/slog"

	"github.com/orbitproxy/orbitproxy-go/internal/sdklog"
	"github.com/orbitproxy/orbitproxy-go/internal/stream"
	"github.com/orbitproxy/orbitproxy-go/wire"
)

// TCPJoinHandler dials the local address and bidirectionally copies bytes (Forward mode).
type TCPJoinHandler struct{}

// InWorkConn handles one work connection.
func (TCPJoinHandler) InWorkConn(logger *slog.Logger, workStream net.Conn, start *wire.StartWorkConn, rt *Runtime) {
	if logger == nil {
		logger = sdklog.Nop()
	}
	cfg := rt.Config()
	if cfg == nil {
		return
	}
	localAddr := strings.TrimSpace(cfg.LocalAddr)
	if localAddr == "" {
		var err error
		localAddr, err = requiredStringField(cfg.LocalServicePayload, "localAddr")
		if err != nil {
			logger.Warn("resolve local service payload failed",
				"proxy_id", start.ProxyID,
				"endpoint_id", start.EndpointID,
				"err", err,
			)
			return
		}
	}
	localConn, err := net.Dial("tcp", localAddr)
	if err != nil {
		logger.Warn("dial local address failed",
			"proxy_id", start.ProxyID,
			"local_addr", localAddr,
			"err", err,
		)
		if cfg.HealthEnabled {
			rt.ReportUnhealthy(err.Error())
		}
		return
	}

	logger.Debug("work conn started",
		"proxy_id", start.ProxyID,
		"endpoint_id", start.EndpointID,
		"local_addr", localAddr,
	)

	wrapped, recycle, err := stream.WrapWorkConn(workStream, stream.WorkConnWrapOptions{})
	if err != nil {
		_ = localConn.Close()
		_ = workStream.Close()
		logger.Warn("wrap work conn failed", "proxy_id", start.ProxyID, "err", err)
		return
	}
	defer recycle()

	inCount, outCount, _ := stream.Join(wrapped, localConn)
	logger.Debug("work conn closed",
		"proxy_id", start.ProxyID,
		"endpoint_id", start.EndpointID,
		"bytes_in", inCount,
		"bytes_out", outCount,
	)
}

func requiredStringField(raw json.RawMessage, key string) (string, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("unmarshal payload: %w", err)
	}
	value, _ := payload[key].(string)
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("payload.%s is required", key)
	}
	return value, nil
}
