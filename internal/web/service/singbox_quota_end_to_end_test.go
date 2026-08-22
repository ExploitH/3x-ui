package service

import (
	"context"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestSingboxAccountingToNodeQuotaMutationIsNodeScoped(t *testing.T) {
	setupConflictDB(t)
	previousManager := runtime.GetManager()
	manager := runtime.NewManager(runtime.LocalDeps{APIPort: func() int { return 0 }, SetNeedRestart: func() {}})
	runtime.SetManager(manager)
	t.Cleanup(func() { runtime.SetManager(previousManager) })

	db := database.GetDB()
	node1 := model.Node{Name: "singbox-quota-node-1", Address: "127.0.0.1", Port: 2096, ApiToken: "tok", Enable: true, Status: "online"}
	node2 := model.Node{Name: "singbox-quota-node-2", Address: "127.0.0.1", Port: 2097, ApiToken: "tok", Enable: true, Status: "online"}
	if err := db.Create(&node1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&node2).Error; err != nil {
		t.Fatal(err)
	}
	fake1, fake2 := &fakeNodeRuntime{}, &fakeNodeRuntime{}
	manager.SetRuntimeOverride(node1.Id, fake1)
	manager.SetRuntimeOverride(node2.Id, fake2)

	client := model.Client{ID: "singbox-quota-client", Email: "alice@example.com", Enable: true}
	nodeInbound(t, node1.Id, 44001, []model.Client{client})
	nodeInbound(t, node2.Id, 44002, []model.Client{client})
	var record model.ClientRecord
	if err := db.Where("email = ?", client.Email).First(&record).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&xray.ClientTraffic{Email: client.Email, Enable: true, Total: 50_000}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientNodeQuota{ClientId: record.Id, NodeId: node1.Id, TotalBytes: 100, ResetPolicy: "never"}).Error; err != nil {
		t.Fatal(err)
	}

	svc := &InboundService{}
	first := singboxServiceSnapshot(100, 100, 1, 1)
	second := singboxServiceSnapshot(150, 150, 2, 2)
	if _, err := svc.ApplySingboxTrafficSnapshot(node1.Id, first); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplySingboxTrafficSnapshot(node2.Id, first); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplySingboxTrafficSnapshot(node1.Id, second); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplySingboxTrafficSnapshot(node2.Id, second); err != nil {
		t.Fatal(err)
	}

	if err := svc.ApplyPendingNodeQuotaBlocksForNode(context.Background(), node1.Id); err != nil {
		t.Fatal(err)
	}
	if got := fake1.updateUser.Load(); got != 1 {
		t.Fatalf("node1 UpdateUser calls=%d, want 1", got)
	}
	if got := fake2.updateUser.Load(); got != 0 {
		t.Fatalf("node2 UpdateUser calls=%d, want 0", got)
	}
	var state model.ClientNodeAccessState
	if err := db.Where("client_id = ? AND node_id = ?", record.Id, node1.Id).First(&state).Error; err != nil {
		t.Fatal(err)
	}
	if !state.Blocked || state.AppliedAt <= 0 {
		t.Fatalf("node1 state=%+v", state)
	}
	var global xray.ClientTraffic
	if err := db.Where("email = ?", client.Email).First(&global).Error; err != nil {
		t.Fatal(err)
	}
	if !global.Enable || global.Up != 0 || global.Down != 0 {
		t.Fatalf("global traffic changed by node-local quota: %+v", global)
	}
}
