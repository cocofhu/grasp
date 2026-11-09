//go:build !linux

package sandbox

import "context"

func newLocalProcessStop(int, <-chan struct{}) localBackendStop {
	return func(context.Context) error { return ErrLocalStopUnconfirmed }
}
