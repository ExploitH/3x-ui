package service

import (
	"context"
	"hash/fnv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

type quotaRestoreRuntime struct {
	*fakeNodeRuntime
	mu      sync.Mutex
	enables []bool
}

func (f *quotaRestoreRuntime) UpdateUser(_ context.Context, _ *model.Inbound, _ string, payload model.Client) error {
	f.updateUser.Add(1)
	f.mu.Lock()
	f.enables = append(f.enables, payload.Enable)
	f.mu.Unlock()
	return nil
}

func (f *quotaRestoreRuntime) enablePayloads() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bool(nil), f.enables...)
}

func seedNodeQuotaResetFixture(t *testing.T, email string, recordEnable, trafficEnable bool, total, up, down, expiry int64) (int, *quotaRestoreRuntime, model.ClientRecord) {
	t.Helper()
	setupConflictDB(t)
	nodeID, base := setupNodeRuntime(t)
	recorder := &quotaRestoreRuntime{fakeNodeRuntime: base}
	runtime.GetManager().SetRuntimeOverride(nodeID, recorder)

	client := model.Client{ID: uuid.NewString(), Email: email, Enable: recordEnable, ExpiryTime: expiry, TotalGB: total}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(email))
	port := 44_000 + int(hasher.Sum32()%1_000)
	ib := nodeInbound(t, nodeID, port, []model.Client{client})
	db := database.GetDB()
	var record model.ClientRecord
	if err := db.Where("email = ?", email).First(&record).Error; err != nil {
		t.Fatalf("read canonical client: %v", err)
	}
	if err := db.Model(&model.ClientRecord{}).Where("id = ?", record.Id).
		Updates(map[string]any{"enable": recordEnable, "expiry_time": expiry, "total_gb": total}).Error; err != nil {
		t.Fatalf("set canonical state: %v", err)
	}
	record.Enable = recordEnable
	record.ExpiryTime = expiry
	record.TotalGB = total

	if err := db.Create(&xray.ClientTraffic{
		InboundId: ib.Id, Email: email, Enable: trafficEnable,
		Total: total, Up: up, Down: down, ExpiryTime: expiry,
	}).Error; err != nil {
		t.Fatalf("create global traffic: %v", err)
	}
	return nodeID, recorder, record
}

func TestResetClientNodeTrafficRestoresOnlyTargetNode(t *testing.T) {
	nodeID, recorder, record := seedNodeQuotaResetFixture(t, "reset-target@x", true, true, 10_000, 500, 700, 0)
	db := database.GetDB()
	const otherNodeID = 9002

	for _, node := range []int{nodeID, otherNodeID} {
		if err := db.Create(&model.ClientNodeQuota{
			ClientId: record.Id, NodeId: node, TotalBytes: 1_000, ResetPolicy: "never", ResetDay: 1,
		}).Error; err != nil {
			t.Fatalf("create quota for node %d: %v", node, err)
		}
		if err := db.Create(&model.ClientNodeUsage{
			ClientId: record.Id, NodeId: node, Up: 600, Down: 500, CycleStartedAt: 100,
		}).Error; err != nil {
			t.Fatalf("create usage for node %d: %v", node, err)
		}
		if err := db.Create(&model.ClientNodeAccessState{
			ClientId: record.Id, NodeId: node, Blocked: true,
			Reason: model.ClientNodeAccessReasonQuotaExhausted, BlockedAt: 200, AppliedAt: 300,
		}).Error; err != nil {
			t.Fatalf("create access state for node %d: %v", node, err)
		}
	}
	baseline := model.NodeClientTraffic{NodeId: nodeID, Email: record.Email, Up: 9_000, Down: 8_000}
	if err := db.Create(&baseline).Error; err != nil {
		t.Fatalf("create raw baseline: %v", err)
	}

	pending, err := (&InboundService{}).ResetClientNodeTraffic(context.Background(), record.Email, nodeID)
	if err != nil {
		t.Fatalf("ResetClientNodeTraffic: %v", err)
	}
	if pending {
		t.Fatal("online target restore unexpectedly remained pending")
	}

	var targetUsage, otherUsage model.ClientNodeUsage
	if err := db.Where("client_id = ? AND node_id = ?", record.Id, nodeID).First(&targetUsage).Error; err != nil {
		t.Fatalf("read target usage: %v", err)
	}
	if err := db.Where("client_id = ? AND node_id = ?", record.Id, otherNodeID).First(&otherUsage).Error; err != nil {
		t.Fatalf("read other usage: %v", err)
	}
	if targetUsage.Up != 0 || targetUsage.Down != 0 || targetUsage.CycleStartedAt <= 100 {
		t.Fatalf("target usage after reset = up:%d down:%d cycle:%d", targetUsage.Up, targetUsage.Down, targetUsage.CycleStartedAt)
	}
	if otherUsage.Up != 600 || otherUsage.Down != 500 || otherUsage.CycleStartedAt != 100 {
		t.Fatalf("other node usage changed = %+v", otherUsage)
	}

	var targetState, otherState model.ClientNodeAccessState
	if err := db.Where("client_id = ? AND node_id = ?", record.Id, nodeID).First(&targetState).Error; err != nil {
		t.Fatalf("read target state: %v", err)
	}
	if err := db.Where("client_id = ? AND node_id = ?", record.Id, otherNodeID).First(&otherState).Error; err != nil {
		t.Fatalf("read other state: %v", err)
	}
	if targetState.Blocked || targetState.AppliedAt <= 0 || targetState.LastError != "" {
		t.Fatalf("target state after reset = blocked:%v applied:%d error:%q", targetState.Blocked, targetState.AppliedAt, targetState.LastError)
	}
	if !otherState.Blocked || otherState.AppliedAt != 300 {
		t.Fatalf("other node state changed = %+v", otherState)
	}

	if got := recorder.enablePayloads(); len(got) != 1 || !got[0] {
		t.Fatalf("remote restore payloads = %v, want [true]", got)
	}
	var raw model.NodeClientTraffic
	if err := db.Where("node_id = ? AND email = ?", nodeID, record.Email).First(&raw).Error; err != nil {
		t.Fatalf("raw NodeClientTraffic baseline was deleted: %v", err)
	}
	if raw.Up != 9_000 || raw.Down != 8_000 {
		t.Fatalf("raw baseline changed = %d/%d", raw.Up, raw.Down)
	}
	var global xray.ClientTraffic
	if err := db.Where("email = ?", record.Email).First(&global).Error; err != nil {
		t.Fatalf("read global traffic: %v", err)
	}
	if !global.Enable || global.Up != 500 || global.Down != 700 {
		t.Fatalf("global traffic changed = enable:%v up:%d down:%d", global.Enable, global.Up, global.Down)
	}
}

func TestResetAllClientNodeTrafficResetsEveryPhysicalNodeOnly(t *testing.T) {
	node1, recorder1, record := seedNodeQuotaResetFixture(t, "reset-all-nodes@x", true, true, 50_000, 1_000, 2_000, 0)
	db := database.GetDB()
	node2Record := model.Node{
		Name: "reset-all-node-2", Address: "127.0.0.1", Port: 2098,
		ApiToken: "tok", Enable: true, Status: "online",
	}
	if err := db.Create(&node2Record).Error; err != nil {
		t.Fatal(err)
	}
	recorder2 := &quotaRestoreRuntime{fakeNodeRuntime: &fakeNodeRuntime{}}
	runtime.GetManager().SetRuntimeOverride(node2Record.Id, recorder2)
	nodeInbound(t, node2Record.Id, 44002, []model.Client{*record.ToClient()})

	for _, nodeID := range []int{node1, node2Record.Id} {
		if err := db.Create(&model.ClientNodeQuota{ClientId: record.Id, NodeId: nodeID, TotalBytes: 100}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.ClientNodeUsage{ClientId: record.Id, NodeId: nodeID, Up: 80, Down: 20, CycleStartedAt: 1}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.ClientNodeAccessState{
			ClientId: record.Id, NodeId: nodeID, Blocked: true,
			Reason: model.ClientNodeAccessReasonQuotaExhausted, AppliedAt: 10,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.NodeClientTraffic{NodeId: nodeID, Email: record.Email, Up: 700, Down: 800}).Error; err != nil {
			t.Fatal(err)
		}
	}

	pendingNodes, err := (&InboundService{}).ResetAllClientNodeTraffic(context.Background(), record.Email)
	if err != nil {
		t.Fatalf("ResetAllClientNodeTraffic: %v", err)
	}
	if len(pendingNodes) != 0 {
		t.Fatalf("online reset-all left pending nodes: %v", pendingNodes)
	}
	for _, nodeID := range []int{node1, node2Record.Id} {
		var usage model.ClientNodeUsage
		if err := db.Where("client_id = ? AND node_id = ?", record.Id, nodeID).First(&usage).Error; err != nil {
			t.Fatal(err)
		}
		if usage.Up != 0 || usage.Down != 0 || usage.CycleStartedAt <= 1 {
			t.Fatalf("node %d usage after reset-all = %+v", nodeID, usage)
		}
		var state model.ClientNodeAccessState
		if err := db.Where("client_id = ? AND node_id = ?", record.Id, nodeID).First(&state).Error; err != nil {
			t.Fatal(err)
		}
		if state.Blocked || state.AppliedAt <= 0 {
			t.Fatalf("node %d state after reset-all = %+v", nodeID, state)
		}
		var baseline model.NodeClientTraffic
		if err := db.Where("node_id = ? AND email = ?", nodeID, record.Email).First(&baseline).Error; err != nil {
			t.Fatalf("node %d baseline was removed: %v", nodeID, err)
		}
	}
	if got := recorder1.enablePayloads(); len(got) != 1 || !got[0] {
		t.Fatalf("node1 restore payloads = %v", got)
	}
	if got := recorder2.enablePayloads(); len(got) != 1 || !got[0] {
		t.Fatalf("node2 restore payloads = %v", got)
	}
	var global xray.ClientTraffic
	if err := db.Where("email = ?", record.Email).First(&global).Error; err != nil {
		t.Fatal(err)
	}
	if global.Up != 1_000 || global.Down != 2_000 || !global.Enable {
		t.Fatalf("global traffic changed by reset-all-nodes: %+v", global)
	}
}

func TestResetClientNodeTrafficOfflineRestoreRetriesWhenNodeReturns(t *testing.T) {
	nodeID, recorder, record := seedNodeQuotaResetFixture(t, "offline-reset@x", true, true, 10_000, 10, 20, 0)
	db := database.GetDB()
	if err := db.Create(&model.ClientNodeQuota{ClientId: record.Id, NodeId: nodeID, TotalBytes: 100}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientNodeUsage{ClientId: record.Id, NodeId: nodeID, Up: 100, Down: 0}).Error; err != nil {
		t.Fatal(err)
	}
	state := model.ClientNodeAccessState{
		ClientId: record.Id, NodeId: nodeID, Blocked: true,
		Reason: model.ClientNodeAccessReasonQuotaExhausted, AppliedAt: 10,
	}
	if err := db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Node{}).Where("id = ?", nodeID).Update("status", "offline").Error; err != nil {
		t.Fatal(err)
	}

	pending, err := (&InboundService{}).ResetClientNodeTraffic(context.Background(), record.Email, nodeID)
	if err != nil {
		t.Fatalf("ResetClientNodeTraffic: %v", err)
	}
	if !pending {
		t.Fatal("offline restore did not remain pending")
	}
	if got := recorder.enablePayloads(); len(got) != 0 {
		t.Fatalf("offline node received payloads: %v", got)
	}
	if err := db.Where("id = ?", state.Id).First(&state).Error; err != nil {
		t.Fatal(err)
	}
	if state.Blocked || state.AppliedAt != 0 || state.LastError == "" {
		t.Fatalf("offline pending state = %+v", state)
	}

	if err := db.Model(&model.Node{}).Where("id = ?", nodeID).Update("status", "online").Error; err != nil {
		t.Fatal(err)
	}
	if err := (&InboundService{}).ApplyPendingNodeQuotaBlocksForNode(context.Background(), nodeID); err != nil {
		t.Fatalf("retry restore: %v", err)
	}
	if got := recorder.enablePayloads(); len(got) != 1 || !got[0] {
		t.Fatalf("retry payloads = %v, want [true]", got)
	}
	if err := db.Where("id = ?", state.Id).First(&state).Error; err != nil {
		t.Fatal(err)
	}
	if state.AppliedAt <= 0 || state.LastError != "" {
		t.Fatalf("retried state = %+v", state)
	}
}

func TestResetClientNodeTrafficNeverRestoresGloballyInvalidClient(t *testing.T) {
	cases := []struct {
		name          string
		recordEnable  bool
		trafficEnable bool
		total         int64
		up            int64
		down          int64
		expiry        int64
	}{
		{name: "admin disabled", recordEnable: false, trafficEnable: false, total: 10_000, up: 10, down: 20},
		{name: "expired before global sweep", recordEnable: true, trafficEnable: true, total: 10_000, up: 10, down: 20, expiry: time.Now().Add(-time.Hour).UnixMilli()},
		{name: "global quota exhausted before global sweep", recordEnable: true, trafficEnable: true, total: 1_000, up: 600, down: 400},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodeID, recorder, record := seedNodeQuotaResetFixture(t, "invalid-reset-"+uuid.NewString()+"@x", tc.recordEnable, tc.trafficEnable, tc.total, tc.up, tc.down, tc.expiry)
			db := database.GetDB()
			if err := db.Create(&model.ClientNodeQuota{ClientId: record.Id, NodeId: nodeID, TotalBytes: 100}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.ClientNodeUsage{ClientId: record.Id, NodeId: nodeID, Up: 80, Down: 20, CycleStartedAt: int64(index + 1)}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.ClientNodeAccessState{
				ClientId: record.Id, NodeId: nodeID, Blocked: true,
				Reason: model.ClientNodeAccessReasonQuotaExhausted, AppliedAt: 10,
			}).Error; err != nil {
				t.Fatal(err)
			}

			pending, err := (&InboundService{}).ResetClientNodeTraffic(context.Background(), record.Email, nodeID)
			if err != nil {
				t.Fatalf("ResetClientNodeTraffic: %v", err)
			}
			if pending {
				t.Fatal("globally invalid client should not queue a node enable mutation")
			}
			if got := recorder.enablePayloads(); len(got) != 0 {
				t.Fatalf("globally invalid client received remote enable payloads: %v", got)
			}
			var state model.ClientNodeAccessState
			if err := db.Where("client_id = ? AND node_id = ?", record.Id, nodeID).First(&state).Error; err != nil {
				t.Fatal(err)
			}
			if state.Blocked || state.AppliedAt <= 0 {
				t.Fatalf("quota state not cleared safely: %+v", state)
			}
		})
	}
}
