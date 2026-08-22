package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

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
