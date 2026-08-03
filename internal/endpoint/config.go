package endpoint

import (
	"encoding/json"

	"github.com/orbitproxy/orbitproxy-go/wire"
)

// Config is a local endpoint configuration snapshot from NewEndpoint.
type Config struct {
	EndpointID            string
	ProxyID               string
	ProxyType             string
	Protocol              string
	Delivery              string
	LocalAddr             string
	LocalServicePayload   json.RawMessage
	HealthEnabled         bool
	HealthIntervalSeconds int
	HealthTimeoutSeconds  int
	HealthMaxFailed       int
}

func configFromNewEndpoint(in *wire.NewEndpoint) *Config {
	if in == nil {
		return nil
	}
	p := parseLocalServicePayload(in.LocalServicePayload)
	return &Config{
		EndpointID:            in.EndpointID,
		ProxyID:               in.ProxyID,
		ProxyType:             in.ProxyType,
		Protocol:              in.Protocol,
		Delivery:              ResolveDelivery(in.LocalServicePayload),
		LocalAddr:             p.LocalAddr,
		LocalServicePayload:   in.LocalServicePayload,
		HealthEnabled:         in.HealthEnabled,
		HealthIntervalSeconds: in.HealthIntervalSeconds,
		HealthTimeoutSeconds:  in.HealthTimeoutSeconds,
		HealthMaxFailed:       in.HealthMaxFailed,
	}
}
