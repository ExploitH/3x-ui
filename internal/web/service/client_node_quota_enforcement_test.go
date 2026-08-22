package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

type quotaApplyRuntime struct {
	*fakeNodeRuntime
	updateErr error
}

func (f *quotaApplyRuntime) UpdateUser(_ context.Context, _ *model.Inbound, _ string, _ model.Client) error {
	f.updateUser.Add(1)
	return f.updateErr
}

func TestApplyPendingNodeQuotaBlocksForNodeScopesMutations(t *testing.T) {
	setupConflictDB(t)
	node1, fake1 := setupNodeRuntime(t)

	db := database.GetDB()
	node2Record := model.Node{
		Name: "quota-node-2", Address: "127.0.0.1", Port: 2097,
		ApiToken: "tok", Enable: true, Status: "online",
	}
	if err := db.Create(&node2Record).Error; err != nil {
		t.Fatalf("create node2: %v", err)
	}
	fake2 := &fakeNodeRuntime{}
	runtime.GetManager().SetRuntimeOverride(node2Record.Id, fake2)

	seed := func(nodeID, port int, email string) model.ClientNodeAccessState {
		t.Helper()
		client := model.Client{ID: uuid.NewString(), Email: email, Enable: true}
		nodeInbound(t, nodeID, port, []model.Client{client})
		var record model.ClientRecord
		if err := db.Where("email = ?", email).First(&record).Error; err != nil {
			t.Fatalf("read client %q: %v", email, err)
		}
		state := model.ClientNodeAccessState{
			ClientId: record.Id, NodeId: nodeID, Blocked: true,
			Reason: model.ClientNodeAccessReasonQuotaExhausted,
		}
		if err := db.Create(&state).Error; err != nil {
			t.Fatalf("create state %q: %v", email, err)
		}
		return state
	}
	state1 := seed(node1, 43201, "scoped-node-1@x")
	state2 := seed(node2Record.Id, 43202, "scoped-node-2@x")

	if err := (&InboundService{}).ApplyPendingNodeQuotaBlocksForNode(context.Background(), node1); err != nil {
		t.Fatalf("apply node1: %v", err)
	}
	if fake1.updateUser.Load() != 1 || fake2.updateUser.Load() != 0 {
		t.Fatalf("scoped calls after node1 = %d/%d, want 1/0", fake1.updateUser.Load(), fake2.updateUser.Load())
	}
	if err := db.Where("id = ?", state1.Id).First(&state1).Error; err != nil {
		t.Fatalf("read state1: %v", err)
	}
	if err := db.Where("id = ?", state2.Id).First(&state2).Error; err != nil {
		t.Fatalf("read state2: %v", err)
	}
	if state1.AppliedAt <= 0 || state2.AppliedAt != 0 {
		t.Fatalf("scoped applied timestamps = %d/%d", state1.AppliedAt, state2.AppliedAt)
	}
}

func TestApplyPendingNodeQuotaBlocksRetriesFailedRemoteMutation(t *testing.T) {
	setupConflictDB(t)
	nodeID, base := setupNodeRuntime(t)
	failing := &quotaApplyRuntime{fakeNodeRuntime: base, updateErr: errors.New("temporary node failure")}
	runtime.GetManager().SetRuntimeOverride(nodeID, failing)

	client := model.Client{ID: uuid.NewString(), Email: "retry-node-block@x", Enable: true}
	nodeInbound(t, nodeID, 43101, []model.Client{client})

	db := database.GetDB()
	var record model.ClientRecord
	if err := db.Where("email = ?", client.Email).First(&record).Error; err != nil {
		t.Fatalf("read canonical client: %v", err)
	}
	state := model.ClientNodeAccessState{
		ClientId: record.Id,
		NodeId:   nodeID,
		Blocked:  true,
		Reason:   model.ClientNodeAccessReasonQuotaExhausted,
	}
	if err := db.Create(&state).Error; err != nil {
		t.Fatalf("create pending state: %v", err)
	}

	if err := (&InboundService{}).ApplyPendingNodeQuotaBlocks(context.Background()); err == nil {
		t.Fatal("remote failure was not returned")
	}
	if err := db.Where("id = ?", state.Id).First(&state).Error; err != nil {
		t.Fatalf("read failed state: %v", err)
	}
	if state.AppliedAt != 0 || state.LastError == "" {
		t.Fatalf("failed state = applied:%d error:%q", state.AppliedAt, state.LastError)
	}

	failing.updateErr = nil
	if err := (&InboundService{}).ApplyPendingNodeQuotaBlocks(context.Background()); err != nil {
		t.Fatalf("retry pending block: %v", err)
	}
	if err := db.Where("id = ?", state.Id).First(&state).Error; err != nil {
		t.Fatalf("read retried state: %v", err)
	}
	if state.AppliedAt <= 0 || state.LastError != "" {
		t.Fatalf("retried state = applied:%d error:%q", state.AppliedAt, state.LastError)
	}
	if got := base.updateUser.Load(); got != 2 {
		t.Fatalf("UpdateUser attempts = %d, want failed attempt + successful retry", got)
	}

	if err := (&InboundService{}).ApplyPendingNodeQuotaBlocks(context.Background()); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}
	if got := base.updateUser.Load(); got != 2 {
		t.Fatalf("already-applied state was retried, calls=%d", got)
	}
}

func TestApplyPendingNodeQuotaBlocksMutatesOnlyMatchingNodeInbounds(t *testing.T) {
	setupConflictDB(t)
	nodeID, fake := setupNodeRuntime(t)

	client := model.Client{
		ID: uuid.NewString(), Email: "node-block@x", SubID: "node-block", Enable: true,
	}
	ib1 := nodeInbound(t, nodeID, 43001, []model.Client{client})
	ib2 := nodeInbound(t, nodeID, 43002, []model.Client{client})

	db := database.GetDB()
	var record model.ClientRecord
	if err := db.Where("email = ?", client.Email).First(&record).Error; err != nil {
		t.Fatalf("read canonical client: %v", err)
	}
	if err := db.Create(&xray.ClientTraffic{
		InboundId: ib1.Id, Email: client.Email, Enable: true, Total: 50_000,
	}).Error; err != nil {
		t.Fatalf("create global traffic: %v", err)
	}
	if err := db.Create(&model.ClientNodeAccessState{
		ClientId: record.Id,
		NodeId:   nodeID,
		Blocked:  true,
		Reason:   model.ClientNodeAccessReasonQuotaExhausted,
	}).Error; err != nil {
		t.Fatalf("create pending access state: %v", err)
	}

	if err := (&InboundService{}).ApplyPendingNodeQuotaBlocks(context.Background()); err != nil {
		t.Fatalf("ApplyPendingNodeQuotaBlocks: %v", err)
	}
	if got := fake.updateUser.Load(); got != 2 {
		t.Fatalf("remote UpdateUser calls = %d, want one for each of 2 matching inbounds", got)
	}

	var state model.ClientNodeAccessState
	if err := db.Where("client_id = ? AND node_id = ?", record.Id, nodeID).First(&state).Error; err != nil {
		t.Fatalf("read access state: %v", err)
	}
	if !state.Blocked || state.AppliedAt <= 0 || state.LastError != "" {
		t.Fatalf("applied state = blocked:%v applied:%d error:%q", state.Blocked, state.AppliedAt, state.LastError)
	}

	var canonical model.ClientRecord
	if err := db.Where("id = ?", record.Id).First(&canonical).Error; err != nil {
		t.Fatalf("read canonical client after apply: %v", err)
	}
	var global xray.ClientTraffic
	if err := db.Where("email = ?", client.Email).First(&global).Error; err != nil {
		t.Fatalf("read global traffic after apply: %v", err)
	}
	if !canonical.Enable || !global.Enable {
		t.Fatalf("node block changed global desired state: client=%v traffic=%v", canonical.Enable, global.Enable)
	}

	// A later full-node reconcile must carry the node-local block without
	// mutating the canonical inbound stored on the master.
	built, err := (&InboundService{}).buildInboundForNodePush(db, ib1)
	if err != nil {
		t.Fatalf("buildInboundForNodePush: %v", err)
	}
	builtClients, err := (&InboundService{}).GetClients(built)
	if err != nil || len(builtClients) != 1 {
		t.Fatalf("read built clients: clients=%d err=%v", len(builtClients), err)
	}
	if builtClients[0].Enable {
		t.Fatal("reconcile payload did not overlay the node-local quota block")
	}

	// The node reports the disabled runtime state on its next snapshot. That
	// node-local false must not flow back into master settings or the shared
	// ClientTraffic.Enable field.
	disabledClient := client
	disabledClient.Enable = false
	remoteInbounds := make([]*model.Inbound, 0, 2)
	for _, source := range []*model.Inbound{ib1, ib2} {
		remote := *source
		remote.Settings = clientsSettings(t, []model.Client{disabledClient})
		remote.ClientStats = []xray.ClientTraffic{{
			Email: client.Email, Enable: false, Total: 50_000, Up: 100, Down: 100,
		}}
		remoteInbounds = append(remoteInbounds, &remote)
	}
	if _, err := (&InboundService{}).setRemoteTrafficLocked(nodeID, &runtime.TrafficSnapshot{Inbounds: remoteInbounds}, false); err != nil {
		t.Fatalf("setRemoteTrafficLocked: %v", err)
	}
	if got := readTraffic(t, db, client.Email); !got.Enable {
		t.Fatal("node quota snapshot flowed back into global ClientTraffic.Enable")
	}

	for _, ib := range []*model.Inbound{ib1, ib2} {
		stored, err := (&InboundService{}).GetInbound(ib.Id)
		if err != nil {
			t.Fatalf("read inbound %d: %v", ib.Id, err)
		}
		clients, err := (&InboundService{}).GetClients(stored)
		if err != nil || len(clients) != 1 {
			t.Fatalf("read inbound clients %d: clients=%d err=%v", ib.Id, len(clients), err)
		}
		if !clients[0].Enable {
			t.Fatalf("master canonical inbound %d was persisted disabled", ib.Id)
		}
	}
}
