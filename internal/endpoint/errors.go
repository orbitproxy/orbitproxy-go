package endpoint

import "errors"

var (
	errNotReady        = errors.New("endpoint not ready")
	errNotInProcess    = errors.New("endpoint is not in_process delivery")
	errListenerClaimed = errors.New("endpoint listener already claimed")
	errEndpointMissing = errors.New("endpoint not found")
)

// Exported for Service.Listen error matching.
var (
	ErrNotReady        = errNotReady
	ErrNotInProcess    = errNotInProcess
	ErrListenerClaimed = errListenerClaimed
	ErrEndpointMissing = errEndpointMissing
)
