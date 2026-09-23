package endpoint

import (
	"context"
	"testing"

	"github.com/orbitproxy/orbitproxy-go/internal/health"
	"github.com/orbitproxy/orbitproxy-go/wire"
)

func TestReportHealthOnlyProbeUpdates(t *testing.T) {
	var got []*wire.EndpointHealth
	rt := NewRuntime(context.Background(), &Config{
		EndpointID:    "ep-mysql",
		Delivery:      DeliveryExec,
		HealthEnabled: true,
	}, func(msg *wire.EndpointHealth) error {
		got = append(got, msg)
		return nil
	})
	defer rt.Close()

	rt.MarkUnhealthy(health.Unhealthy("exited_on_start", "subprocess exited", "process"))
	rt.ReportUnhealthy("dial local address failed")
	if len(got) != 0 {
		t.Fatalf("process exit and dial must not update health, got %d messages", len(got))
	}

	rt.MarkUnhealthy(health.Unhealthy("probe_failed", "probe down", "tcp"))
	rt.MarkHealthy("probe")
	if len(got) != 2 {
		t.Fatalf("expected 2 probe updates, got %d", len(got))
	}
	if got[0].Healthy || got[0].EndpointID != "ep-mysql" {
		t.Fatalf("unexpected probe failure report: %+v", got[0])
	}
	if !got[1].Healthy {
		t.Fatalf("expected probe recovery to mark healthy")
	}
}

func TestReportHealthDisabledIgnoresProbe(t *testing.T) {
	var got int
	rt := NewRuntime(context.Background(), &Config{
		EndpointID:    "ep-mysql",
		Delivery:      DeliveryExec,
		HealthEnabled: false,
	}, func(msg *wire.EndpointHealth) error {
		got++
		return nil
	})
	defer rt.Close()

	rt.MarkUnhealthy(health.Unhealthy("probe_failed", "probe down", "probe"))
	rt.MarkUnhealthy(health.Unhealthy("exited_on_start", "subprocess exited", "process"))
	if got != 0 {
		t.Fatalf("health toggle off must not emit EndpointHealth, got %d", got)
	}
}
