package service

import (
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestApplySingboxTrafficSnapshotBuildsBaselineThenAccumulatesNodeUsage(t *testing.T) {
	db := initTrafficTestDB(t)
	client := model.ClientRecord{Email: "alice@example.com", Enable: true}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&xray.ClientTraffic{Email: client.Email, Enable: true}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &InboundService{}

	first := singboxServiceSnapshot(100, 200, 300, 400)
	if _, err := svc.ApplySingboxTrafficSnapshot(7, first); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	var baseline model.NodeClientTraffic
	if err := db.Where("node_id = ? AND email = ?", 7, client.Email).First(&baseline).Error; err != nil {
		t.Fatalf("baseline: %v", err)
	}
	if baseline.Up != 100 || baseline.Down != 200 {
		t.Fatalf("baseline=%+v", baseline)
	}
	var usageCount int64
	if err := db.Model(&model.ClientNodeUsage{}).Count(&usageCount).Error; err != nil {
		t.Fatal(err)
	}
	if usageCount != 0 {
		t.Fatalf("first snapshot imported usage: %d rows", usageCount)
	}

	second := singboxServiceSnapshot(130, 250, 300, 400)
	if _, err := svc.ApplySingboxTrafficSnapshot(7, second); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	var usage model.ClientNodeUsage
	if err := db.Where("client_id = ? AND node_id = ?", client.Id, 7).First(&usage).Error; err != nil {
		t.Fatalf("usage: %v", err)
	}
	if usage.Up != 30 || usage.Down != 50 {
		t.Fatalf("usage=%+v", usage)
	}
}

func TestApplySingboxTrafficSnapshotSeparatesPhysicalNodesAndHandlesReset(t *testing.T) {
	db := initTrafficTestDB(t)
	client := model.ClientRecord{Email: "alice@example.com", Enable: true}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&xray.ClientTraffic{Email: client.Email, Enable: true}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &InboundService{}
	if _, err := svc.ApplySingboxTrafficSnapshot(7, singboxServiceSnapshot(100, 100, 1, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplySingboxTrafficSnapshot(8, singboxServiceSnapshot(500, 600, 1, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplySingboxTrafficSnapshot(7, singboxServiceSnapshot(10, 20, 2, 3)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplySingboxTrafficSnapshot(8, singboxServiceSnapshot(550, 650, 2, 3)); err != nil {
		t.Fatal(err)
	}

	var node7, node8 model.ClientNodeUsage
	if err := db.Where("client_id = ? AND node_id = ?", client.Id, 7).First(&node7).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("client_id = ? AND node_id = ?", client.Id, 8).First(&node8).Error; err != nil {
		t.Fatal(err)
	}
	if node7.Up != 10 || node7.Down != 20 {
		t.Fatalf("node7=%+v", node7)
	}
	if node8.Up != 50 || node8.Down != 50 {
		t.Fatalf("node8=%+v", node8)
	}
}

func TestApplySingboxTrafficSnapshotUnknownIdentityRollsBack(t *testing.T) {
	db := initTrafficTestDB(t)
	client := model.ClientRecord{Email: "alice@example.com", Enable: true}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	svc := &InboundService{}
	if _, err := svc.ApplySingboxTrafficSnapshot(7, singboxServiceSnapshot(100, 200, 1, 1)); err != nil {
		t.Fatal(err)
	}
	_, err := svc.ApplySingboxTrafficSnapshot(7, &runtime.SingboxTrafficSnapshot{
		Source:     "sing-box-v2ray-api",
		CapturedAt: time.Unix(200, 0),
		Users: []runtime.SingboxUserTraffic{
			{Name: "alice@example.com", Uplink: 150, Downlink: 250, Total: 400},
			{Name: "unknown@example.com", Uplink: 1, Downlink: 1, Total: 2},
		},
	})
	if err == nil {
		t.Fatal("unknown identity accepted")
	}
	var baseline model.NodeClientTraffic
	if err := db.Where("node_id = ? AND email = ?", 7, client.Email).First(&baseline).Error; err != nil {
		t.Fatal(err)
	}
	if baseline.Up != 100 || baseline.Down != 200 {
		t.Fatalf("rollback failed, baseline=%+v", baseline)
	}
}

func singboxServiceSnapshot(up, down, inboundUp, inboundDown int64) *runtime.SingboxTrafficSnapshot {
	return &runtime.SingboxTrafficSnapshot{
		Source:     "sing-box-v2ray-api",
		CapturedAt: time.Unix(up+down+inboundUp+inboundDown, 0),
		Users:      []runtime.SingboxUserTraffic{{Name: "alice@example.com", Uplink: up, Downlink: down, Total: up + down}},
		Inbounds:   []runtime.SingboxInboundTraffic{{Tag: "hk3-reality", Uplink: inboundUp, Downlink: inboundDown, Total: inboundUp + inboundDown}},
	}
}
