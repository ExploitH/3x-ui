package service

import (
	"context"
	"errors"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

type managedSingboxRemoteStub struct {
	snapshot        *runtime.SingboxTrafficSnapshot
	err             error
	trafficSnapshot *runtime.SingboxTrafficSnapshot
	trafficErr      error
}

func (s managedSingboxRemoteStub) FetchManagedSingboxTrafficSnapshot(context.Context) (*runtime.SingboxTrafficSnapshot, error) {
	return s.snapshot, s.err
}

func (s managedSingboxRemoteStub) FetchSingboxTrafficSource(context.Context) (*runtime.SingboxTrafficSnapshot, error) {
	return s.trafficSnapshot, s.trafficErr
}

func TestApplySingboxTrafficFromRemoteReadonlySourceUsesTransactionalAccounting(t *testing.T) {
	db := initTrafficTestDB(t)
	client := model.ClientRecord{Email: "alice@example.com", Enable: true}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	svc := &InboundService{}
	first := managedSingboxRemoteStub{trafficSnapshot: singboxServiceSnapshot(100, 200, 1, 1)}
	applied, err := svc.ApplySingboxTrafficFromRemote(context.Background(), 7, first)
	if err != nil || applied {
		t.Fatalf("first apply=%v err=%v", applied, err)
	}
	second := managedSingboxRemoteStub{trafficSnapshot: singboxServiceSnapshot(130, 250, 1, 1)}
	applied, err = svc.ApplySingboxTrafficFromRemote(context.Background(), 7, second)
	if err != nil || !applied {
		t.Fatalf("second apply=%v err=%v", applied, err)
	}
	var usage model.ClientNodeUsage
	if err := db.Where("client_id = ? AND node_id = ?", client.Id, 7).First(&usage).Error; err != nil {
		t.Fatal(err)
	}
	if usage.Up != 30 || usage.Down != 50 {
		t.Fatalf("usage=%+v", usage)
	}
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

func TestApplyManagedSingboxTrafficFromRemoteUnsupportedDoesNotMutate(t *testing.T) {
	db := initTrafficTestDB(t)
	client := model.ClientRecord{Email: "alice@example.com", Enable: true}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (&InboundService{}).ApplyManagedSingboxTrafficFromRemote(context.Background(), 7, managedSingboxRemoteStub{
		err: runtime.ErrManagedSingboxTrafficUnsupported,
	}); err != nil {
		t.Fatal(err)
	}
	var baselineCount, usageCount int64
	if err := db.Model(&model.NodeClientTraffic{}).Count(&baselineCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ClientNodeUsage{}).Count(&usageCount).Error; err != nil {
		t.Fatal(err)
	}
	if baselineCount != 0 || usageCount != 0 {
		t.Fatalf("unsupported path mutated baseline=%d usage=%d", baselineCount, usageCount)
	}
}

func TestApplyManagedSingboxTrafficFromRemoteDelegatesToTransactionalApply(t *testing.T) {
	db := initTrafficTestDB(t)
	client := model.ClientRecord{Email: "alice@example.com", Enable: true}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	svc := &InboundService{}
	applied, err := svc.ApplyManagedSingboxTrafficFromRemote(context.Background(), 7, managedSingboxRemoteStub{
		snapshot: singboxServiceSnapshot(100, 200, 1, 1),
	})
	if err != nil || applied {
		t.Fatalf("first apply=%v err=%v", applied, err)
	}
	if _, err := svc.ApplyManagedSingboxTrafficFromRemote(context.Background(), 7, managedSingboxRemoteStub{
		snapshot: singboxServiceSnapshot(130, 250, 1, 1),
	}); err != nil {
		t.Fatal(err)
	}
	var usage model.ClientNodeUsage
	if err := db.Where("client_id = ? AND node_id = ?", client.Id, 7).First(&usage).Error; err != nil {
		t.Fatal(err)
	}
	if usage.Up != 30 || usage.Down != 50 {
		t.Fatalf("usage=%+v", usage)
	}
}
