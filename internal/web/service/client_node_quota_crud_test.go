package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func createQuotaCRUDNode(t *testing.T, name string, port int) (int, *quotaRestoreRuntime) {
	t.Helper()
	db := database.GetDB()
	node := model.Node{Name: name, Address: "127.0.0.1", Port: port, ApiToken: "tok", Enable: true, Status: "online"}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node %q: %v", name, err)
	}
	rt := &quotaRestoreRuntime{fakeNodeRuntime: &fakeNodeRuntime{}}
	runtime.GetManager().SetRuntimeOverride(node.Id, rt)
	return node.Id, rt
}

func TestReplaceClientNodeQuotasCRUDAndView(t *testing.T) {
	setupConflictDB(t)
	mgr := runtime.NewManager(runtime.LocalDeps{APIPort: func() int { return 0 }, SetNeedRestart: func() {}})
	runtime.SetManager(mgr)
	t.Cleanup(func() { runtime.SetManager(nil) })
	node1, _ := createQuotaCRUDNode(t, "HK", 2101)
	node2, _ := createQuotaCRUDNode(t, "JP", 2102)

	db := database.GetDB()
	record := model.ClientRecord{Email: "quota-crud@x", UUID: uuid.NewString(), Enable: true}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}

	pending, err := (&InboundService{}).ReplaceClientNodeQuotas(context.Background(), record.Email, []ClientNodeQuotaInput{
		{NodeId: node1, TotalBytes: 100, ResetPolicy: "monthly", ResetDay: 15},
		{NodeId: node2, TotalBytes: 0, ResetPolicy: "never", ResetDay: 1},
	})
	if err != nil {
		t.Fatalf("ReplaceClientNodeQuotas: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("unexpected pending nodes: %v", pending)
	}
	if err := db.Create(&model.ClientNodeUsage{ClientId: record.Id, NodeId: node1, Up: 30, Down: 20}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientNodeAccessState{
		ClientId: record.Id, NodeId: node1, Blocked: true,
		Reason: model.ClientNodeAccessReasonQuotaExhausted, BlockedAt: 5, AppliedAt: 6,
	}).Error; err != nil {
		t.Fatal(err)
	}

	views, err := (&InboundService{}).GetClientNodeQuotas(record.Email)
	if err != nil {
		t.Fatalf("GetClientNodeQuotas: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("views=%d, want 2: %+v", len(views), views)
	}
	byNode := map[int]ClientNodeQuotaView{}
	for _, view := range views {
		byNode[view.NodeId] = view
	}
	if got := byNode[node1]; got.NodeName != "HK" || got.TotalBytes != 100 || got.ResetPolicy != "monthly" || got.ResetDay != 15 || got.Up != 30 || got.Down != 20 || !got.Blocked || got.Reason != model.ClientNodeAccessReasonQuotaExhausted {
		t.Fatalf("HK view = %+v", got)
	}
	if got := byNode[node2]; got.NodeName != "JP" || got.TotalBytes != 0 || got.ResetPolicy != "never" {
		t.Fatalf("JP view = %+v", got)
	}

	// Full replacement updates node1 and removes node2's config/accounting.
	if err := db.Create(&model.ClientNodeUsage{ClientId: record.Id, NodeId: node2, Up: 8, Down: 9}).Error; err != nil {
		t.Fatal(err)
	}
	pending, err = (&InboundService{}).ReplaceClientNodeQuotas(context.Background(), record.Email, []ClientNodeQuotaInput{
		{NodeId: node1, TotalBytes: 200, ResetPolicy: "daily", ResetDay: 1},
	})
	if err != nil {
		t.Fatalf("replace second version: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("unexpected pending after delete: %v", pending)
	}
	views, err = (&InboundService{}).GetClientNodeQuotas(record.Email)
	if err != nil || len(views) != 1 {
		t.Fatalf("views after replace = %+v err=%v", views, err)
	}
	if views[0].NodeId != node1 || views[0].TotalBytes != 200 || views[0].ResetPolicy != "daily" {
		t.Fatalf("updated node1 view = %+v", views[0])
	}
	var removedUsage int64
	if err := db.Model(&model.ClientNodeUsage{}).Where("client_id = ? AND node_id = ?", record.Id, node2).Count(&removedUsage).Error; err != nil {
		t.Fatal(err)
	}
	if removedUsage != 0 {
		t.Fatalf("removed node usage rows=%d", removedUsage)
	}
}

func TestReplaceClientNodeQuotasReconcilesRaisedLoweredAndRemovedLimits(t *testing.T) {
	setupConflictDB(t)
	mgr := runtime.NewManager(runtime.LocalDeps{APIPort: func() int { return 0 }, SetNeedRestart: func() {}})
	runtime.SetManager(mgr)
	t.Cleanup(func() { runtime.SetManager(nil) })
	nodeID, recorder := createQuotaCRUDNode(t, "Quota Node", 2201)
	client := model.Client{ID: uuid.NewString(), Email: "quota-resize@x", Enable: true, TotalGB: 10_000}
	ib := nodeInbound(t, nodeID, 45001, []model.Client{client})

	db := database.GetDB()
	var record model.ClientRecord
	if err := db.Where("email = ?", client.Email).First(&record).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&xray.ClientTraffic{InboundId: ib.Id, Email: client.Email, Enable: true, Total: 10_000}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientNodeQuota{ClientId: record.Id, NodeId: nodeID, TotalBytes: 100, ResetPolicy: "never"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientNodeUsage{ClientId: record.Id, NodeId: nodeID, Up: 60, Down: 40}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientNodeAccessState{
		ClientId: record.Id, NodeId: nodeID, Blocked: true,
		Reason: model.ClientNodeAccessReasonQuotaExhausted, AppliedAt: 10,
	}).Error; err != nil {
		t.Fatal(err)
	}

	// Raising the cap above current usage restores only this node.
	if _, err := (&InboundService{}).ReplaceClientNodeQuotas(context.Background(), client.Email, []ClientNodeQuotaInput{
		{NodeId: nodeID, TotalBytes: 200, ResetPolicy: "never", ResetDay: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if got := recorder.enablePayloads(); len(got) != 1 || !got[0] {
		t.Fatalf("raise-cap payloads=%v, want [true]", got)
	}

	// Lowering it below unchanged usage blocks the node again.
	if _, err := (&InboundService{}).ReplaceClientNodeQuotas(context.Background(), client.Email, []ClientNodeQuotaInput{
		{NodeId: nodeID, TotalBytes: 50, ResetPolicy: "never", ResetDay: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if got := recorder.enablePayloads(); len(got) != 2 || got[1] {
		t.Fatalf("lower-cap payloads=%v, want [true false]", got)
	}

	// Removing the quota restores the client and deletes config/usage while the
	// access state remains as non-blocking audit/retry state.
	if _, err := (&InboundService{}).ReplaceClientNodeQuotas(context.Background(), client.Email, nil); err != nil {
		t.Fatal(err)
	}
	if got := recorder.enablePayloads(); len(got) != 3 || !got[2] {
		t.Fatalf("remove-quota payloads=%v, want [true false true]", got)
	}
	var quotas, usages int64
	db.Model(&model.ClientNodeQuota{}).Where("client_id = ?", record.Id).Count(&quotas)
	db.Model(&model.ClientNodeUsage{}).Where("client_id = ?", record.Id).Count(&usages)
	if quotas != 0 || usages != 0 {
		t.Fatalf("removed quota left config/usage rows: quotas=%d usages=%d", quotas, usages)
	}
	var global xray.ClientTraffic
	if err := db.Where("email = ?", client.Email).First(&global).Error; err != nil {
		t.Fatal(err)
	}
	if !global.Enable {
		t.Fatal("quota CRUD changed global enable")
	}
}

func seedQuotaLifecycleRows(t *testing.T, recordID, nodeID int) {
	t.Helper()
	db := database.GetDB()
	if err := db.Create(&model.ClientNodeQuota{ClientId: recordID, NodeId: nodeID, TotalBytes: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientNodeUsage{ClientId: recordID, NodeId: nodeID, Up: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientNodeAccessState{ClientId: recordID, NodeId: nodeID, Blocked: true, Reason: model.ClientNodeAccessReasonQuotaExhausted}).Error; err != nil {
		t.Fatal(err)
	}
}

func assertNoClientQuotaLifecycleRows(t *testing.T, recordID int) {
	t.Helper()
	db := database.GetDB()
	for name, table := range map[string]any{
		"quotas": &model.ClientNodeQuota{}, "usages": &model.ClientNodeUsage{}, "states": &model.ClientNodeAccessState{},
	} {
		var count int64
		if err := db.Model(table).Where("client_id = ?", recordID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("delete left %d %s rows for client %d", count, name, recordID)
		}
	}
}

func TestClientAndNodeDeletionCleanNodeQuotaState(t *testing.T) {
	t.Run("client delete removes all client-scoped quota rows", func(t *testing.T) {
		setupConflictDB(t)
		db := database.GetDB()
		record := model.ClientRecord{Email: "quota-delete-client@x", Enable: true}
		if err := db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.ClientNodeQuota{ClientId: record.Id, NodeId: 91, TotalBytes: 1}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.ClientNodeUsage{ClientId: record.Id, NodeId: 91, Up: 1}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.ClientNodeAccessState{ClientId: record.Id, NodeId: 91, Blocked: true, Reason: model.ClientNodeAccessReasonQuotaExhausted}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := (&ClientService{}).Delete(&InboundService{}, record.Id, true); err != nil {
			t.Fatalf("delete client: %v", err)
		}
		for name, table := range map[string]any{
			"quotas": &model.ClientNodeQuota{}, "usages": &model.ClientNodeUsage{}, "states": &model.ClientNodeAccessState{},
		} {
			var count int64
			if err := db.Model(table).Where("client_id = ?", record.Id).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("client delete left %d %s rows", count, name)
			}
		}
	})

	t.Run("bulk delete removes client-scoped quota rows", func(t *testing.T) {
		setupConflictDB(t)
		db := database.GetDB()
		record := model.ClientRecord{Email: "quota-bulk-delete@x", Enable: true}
		if err := db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
		seedQuotaLifecycleRows(t, record.Id, 92)
		result, _, err := (&ClientService{}).BulkDelete(&InboundService{}, []string{record.Email}, true)
		if err != nil {
			t.Fatalf("bulk delete: %v", err)
		}
		if result.Deleted != 1 {
			t.Fatalf("bulk deleted=%d, want 1; result=%+v", result.Deleted, result)
		}
		assertNoClientQuotaLifecycleRows(t, record.Id)
	})

	t.Run("delete orphans removes client-scoped quota rows", func(t *testing.T) {
		setupConflictDB(t)
		db := database.GetDB()
		record := model.ClientRecord{Email: "quota-delete-orphan@x", Enable: true}
		if err := db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
		seedQuotaLifecycleRows(t, record.Id, 93)
		deleted, err := (&ClientService{}).DeleteOrphans()
		if err != nil {
			t.Fatalf("delete orphans: %v", err)
		}
		if deleted != 1 {
			t.Fatalf("deleted orphans=%d, want 1", deleted)
		}
		assertNoClientQuotaLifecycleRows(t, record.Id)
	})

	t.Run("sync orphan reaper removes client-scoped quota rows", func(t *testing.T) {
		setupConflictDB(t)
		db := database.GetDB()
		record := model.ClientRecord{
			Email: "quota-sync-orphan@x", Enable: true,
			SyncOrphanedAt: time.Now().Add(-syncOrphanReapGrace - time.Minute).UnixMilli(),
		}
		if err := db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
		seedQuotaLifecycleRows(t, record.Id, 94)
		reaped, err := (&ClientService{}).ReapSyncOrphans()
		if err != nil {
			t.Fatalf("reap sync orphans: %v", err)
		}
		if reaped != 1 {
			t.Fatalf("reaped=%d, want 1", reaped)
		}
		assertNoClientQuotaLifecycleRows(t, record.Id)
	})

	t.Run("node delete removes only that physical node quota rows", func(t *testing.T) {
		setupConflictDB(t)
		db := database.GetDB()
		node1 := model.Node{Name: "quota-delete-node-1", Address: "example.com", Port: 443, ApiToken: "tok"}
		node2 := model.Node{Name: "quota-delete-node-2", Address: "example.net", Port: 443, ApiToken: "tok"}
		if err := db.Create(&node1).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&node2).Error; err != nil {
			t.Fatal(err)
		}
		record := model.ClientRecord{Email: "quota-delete-node@x", Enable: true}
		if err := db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
		for _, nodeID := range []int{node1.Id, node2.Id} {
			if err := db.Create(&model.ClientNodeQuota{ClientId: record.Id, NodeId: nodeID, TotalBytes: 1}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.ClientNodeUsage{ClientId: record.Id, NodeId: nodeID, Up: 1}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.ClientNodeAccessState{ClientId: record.Id, NodeId: nodeID}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := (&NodeService{}).Delete(node1.Id); err != nil {
			t.Fatalf("delete node1: %v", err)
		}
		for name, table := range map[string]any{
			"quotas": &model.ClientNodeQuota{}, "usages": &model.ClientNodeUsage{}, "states": &model.ClientNodeAccessState{},
		} {
			var gone, kept int64
			db.Model(table).Where("node_id = ?", node1.Id).Count(&gone)
			db.Model(table).Where("node_id = ?", node2.Id).Count(&kept)
			if gone != 0 || kept != 1 {
				t.Fatalf("%s cleanup gone=%d kept=%d", name, gone, kept)
			}
		}
	})
}

func TestReplaceClientNodeQuotasRejectsInvalidInputAtomically(t *testing.T) {
	setupConflictDB(t)
	mgr := runtime.NewManager(runtime.LocalDeps{APIPort: func() int { return 0 }})
	runtime.SetManager(mgr)
	t.Cleanup(func() { runtime.SetManager(nil) })
	nodeID, _ := createQuotaCRUDNode(t, "valid", 2301)
	db := database.GetDB()
	record := model.ClientRecord{Email: "quota-invalid@x", Enable: true}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientNodeQuota{ClientId: record.Id, NodeId: nodeID, TotalBytes: 123, ResetPolicy: "never", ResetDay: 1}).Error; err != nil {
		t.Fatal(err)
	}

	cases := [][]ClientNodeQuotaInput{
		{{NodeId: nodeID, TotalBytes: -1, ResetPolicy: "never", ResetDay: 1}},
		{{NodeId: nodeID, TotalBytes: 10, ResetPolicy: "invalid", ResetDay: 1}},
		{{NodeId: nodeID, TotalBytes: 10, ResetPolicy: "monthly", ResetDay: 32}},
		{{NodeId: nodeID, TotalBytes: 10, ResetPolicy: "never"}, {NodeId: nodeID, TotalBytes: 20, ResetPolicy: "daily"}},
		{{NodeId: 999_999, TotalBytes: 10, ResetPolicy: "never", ResetDay: 1}},
	}
	for i, inputs := range cases {
		if _, err := (&InboundService{}).ReplaceClientNodeQuotas(context.Background(), record.Email, inputs); err == nil {
			t.Fatalf("invalid case %d was accepted: %+v", i, inputs)
		}
		var quota model.ClientNodeQuota
		if err := db.Where("client_id = ? AND node_id = ?", record.Id, nodeID).First(&quota).Error; err != nil {
			t.Fatal(err)
		}
		if quota.TotalBytes != 123 || quota.ResetPolicy != "never" {
			t.Fatalf("invalid case %d changed existing quota: %+v", i, quota)
		}
	}
}
