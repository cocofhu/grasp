package runtime

import (
	"context"
	"errors"
	"github.com/cocofhu/grasp/internal/sandbox"
)

var ErrProviderDispatchUnknown = errors.New("the original provider operation may have executed; reconcile its result before continuing")

func prepareProviderAttempt(ctx context.Context, req NodeReq) (NodeReq, error) {
	if req.AttachOnly {
		if req.ReattachSandbox == "" || req.Operation.Resource != req.ReattachSandbox || !req.Operation.Valid() || req.ProviderAttempt != req.Operation.Attempt {
			return req, ErrProviderDispatchUnknown
		}
		req.providerAttemptPrepared = true
		return req, nil
	}
	if req.providerAttemptPrepared || req.PrepareProviderAttempt == nil {
		return req, nil
	}
	if req.ReattachSandbox != "" {
		return req, ErrProviderDispatchUnknown
	}
	operation, err := req.PrepareProviderAttempt(ctx)
	if err != nil {
		return req, err
	}
	if operation.Attempt <= 0 || !sandbox.ValidOperationID(operation.ID) {
		return req, ErrProviderDispatchUnknown
	}
	req.ProviderAttempt, req.Operation, req.providerAttemptPrepared = operation.Attempt, operation, true
	return req, nil
}
