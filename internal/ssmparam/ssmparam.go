// Package ssmparam is a thin, test-friendly abstraction over one AWS SSM
// Parameter Store parameter. Stores that persist JSON blobs between Lambda
// invocations build on it.
package ssmparam

import (
	"context"
	"errors"
)

// ErrNotFound is returned by API.GetParameter when the parameter does not
// exist. Callers typically treat it as "empty".
var ErrNotFound = errors.New("parameter not found")

// API is the slice of SSM we need. Tests substitute a fake.
type API interface {
	GetParameter(ctx context.Context, name string) (string, error)
	PutParameter(ctx context.Context, name, value string) error
}
