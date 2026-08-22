package service

import (
	"context"
	"errors"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

type managedSingboxRemoteStub struct {
	snapshot *runtime.SingboxTrafficSnapshot
	err      error
}

func (s managedSingboxRemoteStub) FetchManagedSingboxTrafficSnapshot(context.Context) (*runtime.SingboxTrafficSnapshot, error) {
	return s.snapshot, s.err
}

func TestFetchManagedSingboxTrafficSnapshotSkipsUnsupported(t *testing.T) {
	stub := managedSingboxRemoteStub{err: runtime.ErrManagedSingboxTrafficUnsupported}
	snapshot, supported, err := fetchManagedSingboxTrafficSnapshot(context.Background(), stub)
	if err != nil || supported || snapshot != nil {
		t.Fatalf("snapshot=%+v supported=%v err=%v", snapshot, supported, err)
	}
}

func TestFetchManagedSingboxTrafficSnapshotPropagatesTransportError(t *testing.T) {
	transportErr := errors.New("connection refused")
	stub := managedSingboxRemoteStub{err: transportErr}
	_, supported, err := fetchManagedSingboxTrafficSnapshot(context.Background(), stub)
	if supported || !errors.Is(err, transportErr) {
		t.Fatalf("supported=%v err=%v", supported, err)
	}
}
