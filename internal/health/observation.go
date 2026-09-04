package health

import "time"

// Observation is one health signal — from an active probe failure/success or a
// passive runtime event (process exit, dial error, etc.).
// Product-wise this is still "health check", not a separate diagnostic product.
type Observation struct {
	Healthy bool

	// Code is a stable machine-facing reason code (e.g. exited_on_start, dial_failed).
	Code string
	// Message is a short human-readable summary.
	Message string

	// Optional evidence (passive path often fills these).
	ExitCode   *int
	StderrTail string
	Source     string // "probe" | "process" | "dial" | ...
	ObservedAt time.Time
}

// Unhealthy builds a passive/active failure observation.
func Unhealthy(code, message, source string) Observation {
	return Observation{
		Healthy:    false,
		Code:       code,
		Message:    message,
		Source:     source,
		ObservedAt: time.Now(),
	}
}

// HealthyObs builds a recovery observation.
func HealthyObs(source string) Observation {
	return Observation{
		Healthy:    true,
		Source:     source,
		ObservedAt: time.Now(),
	}
}

// ReasonText prefers Message, then Code.
func (o Observation) ReasonText() string {
	if o.Message != "" {
		return o.Message
	}
	return o.Code
}
