package endpoint

import (
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
		localAddr, err = LocalAddrFromPayload(cfg.LocalServicePayload)
		if err != nil {
			logger.Warn("resolve local service payload failed",
				"endpoint_id", start.EndpointID,
				"err", err,
			)
			return
		}
	}
	localConn, err := net.Dial("tcp", localAddr)
	if err != nil {
		logger.Warn("dial local address failed",
			"stage", "local_dial",
			"endpoint_id", start.EndpointID,
			"local_addr", localAddr,
			"err", err,
		)
		return
	}

	logger.Debug("work conn started",
		"endpoint_id", start.EndpointID,
		"local_addr", localAddr,
	)

	inCount, outCount, _ := stream.Join(workStream, localConn)
	logger.Debug("work conn closed",
		"endpoint_id", start.EndpointID,
		"bytes_in", inCount,
		"bytes_out", outCount,
	)
}
