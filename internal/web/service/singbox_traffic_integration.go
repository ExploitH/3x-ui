package service

import (
	"context"
	"errors"

	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

func fetchManagedSingboxTrafficSnapshot(ctx context.Context, remote interface {
	FetchManagedSingboxTrafficSnapshot(context.Context) (*runtime.SingboxTrafficSnapshot, error)
},
) (*runtime.SingboxTrafficSnapshot, bool, error) {
	if remote == nil {
		return nil, false, errors.New("managed sing-box traffic reader is nil")
	}
	snapshot, err := remote.FetchManagedSingboxTrafficSnapshot(ctx)
	if errors.Is(err, runtime.ErrManagedSingboxTrafficUnsupported) || errors.Is(err, runtime.ErrSingboxTrafficUnsupported) || errors.Is(err, runtime.ErrCapabilitiesUnsupported) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if snapshot == nil {
		return nil, true, errors.New("managed sing-box traffic reader returned nil snapshot")
	}
	return snapshot, true, nil
}

// ApplyManagedSingboxTrafficFromRemote is the future NodeTrafficSyncJob seam.
// Unsupported legacy/readonly nodes are a no-op; supported snapshots enter the
// same transactional accounting path as direct staging calls.
func (s *InboundService) ApplyManagedSingboxTrafficFromRemote(ctx context.Context, nodeID int, remote interface {
	FetchManagedSingboxTrafficSnapshot(context.Context) (*runtime.SingboxTrafficSnapshot, error)
},
) (bool, error) {
	snapshot, supported, err := fetchManagedSingboxTrafficSnapshot(ctx, remote)
	if err != nil || !supported {
		return false, err
	}
	return s.ApplySingboxTrafficSnapshot(nodeID, snapshot)
}
