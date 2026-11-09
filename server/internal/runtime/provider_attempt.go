package runtime

import (
	"context"
	"errors"
)

var ErrProviderDispatchUnknown = errors.New("the original provider operation may have executed; reconcile its result before continuing")

func prepareProviderAttempt(ctx context.Context, req NodeReq) (NodeReq, error) {
	if req.providerAttemptPrepared || req.PrepareProviderAttempt == nil {
		return req, nil
	}
	if req.ReattachSandbox != "" {
		return req, ErrProviderDispatchUnknown
	}
	attempt, err := req.PrepareProviderAttempt(ctx)
	if err != nil {
		return req, err
	}
	if attempt <= 0 {
		return req, ErrProviderDispatchUnknown
	}
	req.ProviderAttempt, req.providerAttemptPrepared = attempt, true
	return req, nil
}
