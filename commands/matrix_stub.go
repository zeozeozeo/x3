//go:build !matrix && !goolm

package commands

import (
	"context"
	"fmt"
	"os"
)

type MatrixRuntime struct {
	Cancel context.CancelFunc
	Done   <-chan struct{}
	Close  func() error
}

func StartMatrixBot(parent context.Context) (*MatrixRuntime, error) {
	if !truthy(os.Getenv("X3_MATRIX_ENABLED")) {
		return nil, nil
	}
	return nil, fmt.Errorf("Matrix support is not built in; rebuild with -tags goolm for the pure-Go E2EE backend")
}

func truthy(value string) bool {
	switch value {
	case "1", "true", "TRUE", "True", "yes", "YES", "on", "ON", "enabled", "ENABLED":
		return true
	default:
		return false
	}
}
