package provider

import "errors"

// ErrNotSupported is returned when a provider does not support an operation.
var ErrNotSupported = errors.New("nexus: operation not supported by provider")
