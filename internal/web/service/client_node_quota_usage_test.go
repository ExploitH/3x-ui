package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestNodeQuotaBoundaryMarksOnlyNodeAccessState(t *testing.T) {
	db := initTrafficTestDB(t)
	createNodeInbound(t, db, 1, "quota-n1", 42001)
	createNodeInbound(t, db, 2, "quota-n2", 42002)

	client := model.ClientRecord{Email: "quota@node", Enable: true}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	if err := db.Create(&xray.ClientTraffic{InboundId: 1, Email: client.Email, Enable: true, Total: 50_000}).Error; err != nil {
		t.Fatalf("create global traffic row: %v", err)
	}
	if err := db.Create(&model.ClientNodeQuota{
		ClientId: client.Id, NodeId: 1, TotalBytes: 1_000, ResetPolicy: "never", ResetDay: 1,
	}).Error; err != nil {
		t.Fatalf("create node quota: %v", err)
	}

	svc := &InboundService{}
	syncNode(t, svc, 1, "quota-n1", xray.ClientTraffic{Email: client.Email, Up: 100, Down: 100, Enable: true})
	syncNode(t, svc, 2, "quota-n2", xray.ClientTraffic{Email: client.Email, Up: 100, Down: 100, Enable: true})

	// Node 1 reaches 800 bytes and remains usable.
	syncNode(t, svc, 1, "quota-n1", xray.ClientTraffic{Email: client.Email, Up: 500, Down: 500, Enable: true})
	var states int64
	if err := db.Model(&model.ClientNodeAccessState{}).Where("client_id = ?", client.Id).Count(&states).Error; err != nil {
		t.Fatalf("count access states: %v", err)
	}
	if states != 0 {
		t.Fatalf("node blocked below quota: %d state rows", states)
	}

	// The next 200 bytes land exactly on the boundary and block only node 1.
	syncNode(t, svc, 1, "quota-n1", xray.ClientTraffic{Email: client.Email, Up: 600, Down: 600, Enable: true})
	// Node 2 can exceed the same amount but has no configured node quota.
	syncNode(t, svc, 2, "quota-n2", xray.ClientTraffic{Email: client.Email, Up: 900, Down: 900, Enable: true})

	var access model.ClientNodeAccessState
	if err := db.Where("client_id = ? AND node_id = ?", client.Id, 1).First(&access).Error; err != nil {
		t.Fatalf("read node1 access state: %v", err)
	}
	if !access.Blocked || access.Reason != model.ClientNodeAccessReasonQuotaExhausted {
		t.Fatalf("node1 access state = blocked:%v reason:%q", access.Blocked, access.Reason)
	}
	if access.BlockedAt <= 0 || access.AppliedAt != 0 {
		t.Fatalf("new block timestamps = blocked:%d applied:%d", access.BlockedAt, access.AppliedAt)
	}

	if err := db.Model(&model.ClientNodeAccessState{}).
		Where("client_id = ? AND node_id = ?", client.Id, 2).
		Count(&states).Error; err != nil {
		t.Fatalf("count node2 state: %v", err)
	}
	if states != 0 {
		t.Fatalf("unlimited node2 received %d block states", states)
	}

	var globalTraffic xray.ClientTraffic
	if err := db.Where("email = ?", client.Email).First(&globalTraffic).Error; err != nil {
		t.Fatalf("read global traffic: %v", err)
	}
	var canonical model.ClientRecord
	if err := db.Where("id = ?", client.Id).First(&canonical).Error; err != nil {
		t.Fatalf("read canonical client: %v", err)
	}
	if !globalTraffic.Enable || !canonical.Enable {
		t.Fatalf("node-only quota changed global enable: traffic=%v client=%v", globalTraffic.Enable, canonical.Enable)
	}
}

func TestNodeTrafficDeltaAccumulatesSeparatedClientNodeUsage(t *testing.T) {
	db := initTrafficTestDB(t)
	createNodeInbound(t, db, 1, "n1-in", 41001)
	createNodeInbound(t, db, 2, "n2-in", 41002)

	client := model.ClientRecord{Email: "alice@node", Enable: true}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	if err := db.Create(&xray.ClientTraffic{InboundId: 1, Email: client.Email, Enable: true}).Error; err != nil {
		t.Fatalf("create global traffic row: %v", err)
	}

	svc := &InboundService{}

	// First snapshots establish raw node baselines; historical counters are not
	// imported into the new quota cycle.
	syncNode(t, svc, 1, "n1-in", xray.ClientTraffic{Email: client.Email, Up: 100, Down: 200, Enable: true})
	syncNode(t, svc, 2, "n2-in", xray.ClientTraffic{Email: client.Email, Up: 500, Down: 600, Enable: true})

	var usageCount int64
	if err := db.Model(&model.ClientNodeUsage{}).Where("client_id = ?", client.Id).Count(&usageCount).Error; err != nil {
		t.Fatalf("count usages: %v", err)
	}
	if usageCount != 0 {
		t.Fatalf("first baselines imported historical traffic into %d usage rows", usageCount)
	}

	// Deltas on each physical node must accumulate independently.
	syncNode(t, svc, 1, "n1-in", xray.ClientTraffic{Email: client.Email, Up: 400, Down: 700, Enable: true}) // +300/+500
	syncNode(t, svc, 2, "n2-in", xray.ClientTraffic{Email: client.Email, Up: 550, Down: 690, Enable: true}) // +50/+90

	var node1, node2 model.ClientNodeUsage
	if err := db.Where("client_id = ? AND node_id = ?", client.Id, 1).First(&node1).Error; err != nil {
		t.Fatalf("read node1 usage: %v", err)
	}
	if err := db.Where("client_id = ? AND node_id = ?", client.Id, 2).First(&node2).Error; err != nil {
		t.Fatalf("read node2 usage: %v", err)
	}
	if node1.Up != 300 || node1.Down != 500 {
		t.Fatalf("node1 usage = %d/%d, want 300/500", node1.Up, node1.Down)
	}
	if node2.Up != 50 || node2.Down != 90 {
		t.Fatalf("node2 usage = %d/%d, want 50/90", node2.Up, node2.Down)
	}
	if node1.CycleStartedAt <= 0 || node2.CycleStartedAt <= 0 {
		t.Fatalf("usage cycles were not initialized: node1=%d node2=%d", node1.CycleStartedAt, node2.CycleStartedAt)
	}

	// A raw node counter reset clamps negative deltas to zero and only moves the
	// upstream NodeClientTraffic baseline; accumulated quota usage is unchanged.
	syncNode(t, svc, 1, "n1-in", xray.ClientTraffic{Email: client.Email, Up: 10, Down: 20, Enable: true})
	if err := db.Where("client_id = ? AND node_id = ?", client.Id, 1).First(&node1).Error; err != nil {
		t.Fatalf("read node1 usage after reset: %v", err)
	}
	if node1.Up != 300 || node1.Down != 500 {
		t.Fatalf("counter reset changed accumulated usage to %d/%d", node1.Up, node1.Down)
	}

	var global xray.ClientTraffic
	if err := db.Where("email = ?", client.Email).First(&global).Error; err != nil {
		t.Fatalf("read global traffic: %v", err)
	}
	if !global.Enable {
		t.Fatal("node usage accounting changed global ClientTraffic.Enable")
	}
}
