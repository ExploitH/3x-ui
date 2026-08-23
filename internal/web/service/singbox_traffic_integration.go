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

func fetchSingboxTrafficSource(ctx context.Context, remote interface {
	FetchSingboxTrafficSource(context.Context) (*runtime.SingboxTrafficSnapshot, error)
},
) (*runtime.SingboxTrafficSnapshot, bool, error) {
	if remote == nil {
		return nil, false, errors.New("sing-box traffic source reader is nil")
	}
	snapshot, err := remote.FetchSingboxTrafficSource(ctx)
	if errors.Is(err, runtime.ErrSingboxTrafficUnsupported) || errors.Is(err, runtime.ErrCapabilitiesUnsupported) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if snapshot == nil {
		return nil, true, errors.New("sing-box traffic source returned nil snapshot")
	}
	return snapshot, true, nil
}

// ApplySingboxTrafficFromRemote is the accounting-only seam. It accepts a
// traffic-readonly adapter, while ApplyManagedSingboxTrafficFromRemote remains
// strict and requires client mutation capabilities for future enforcement.
func (s *InboundService) ApplySingboxTrafficFromRemote(ctx context.Context, nodeID int, remote interface {
	FetchSingboxTrafficSource(context.Context) (*runtime.SingboxTrafficSnapshot, error)
},
) (bool, error) {
	snapshot, supported, err := fetchSingboxTrafficSource(ctx, remote)
	if err != nil || !supported {
		return false, err
	}
	return s.ApplySingboxTrafficSnapshot(nodeID, snapshot)
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
