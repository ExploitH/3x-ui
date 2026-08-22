package service

import (
	"strconv"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestUpsertNodeTrafficCycleIsValidatedAndIdempotent(t *testing.T) {
	setupConflictDB(t)
	db := database.GetDB()
	node := model.Node{Name: "billing-import-node", Address: "example.com", Port: 443, ApiToken: "tok"}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}

	input := NodeTrafficCycleInput{
		NodeId: node.Id, CycleStart: 100, CycleEnd: 200,
		IngressBytes: 11, EgressBytes: 22, ProviderBillableBytes: 33,
		Source: "provider_import", TrafficCalcType: "total",
	}
	if err := UpsertNodeTrafficCycle(db, input); err != nil {
		t.Fatalf("first import: %v", err)
	}
	input.ProviderBillableBytes = 44
	if err := UpsertNodeTrafficCycle(db, input); err != nil {
		t.Fatalf("idempotent update: %v", err)
	}
	var count int64
	if err := db.Model(&model.NodeTrafficCycle{}).Where("node_id = ?", node.Id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("cycle rows=%d, want 1", count)
	}
	var got model.NodeTrafficCycle
	if err := db.Where("node_id = ? AND cycle_start = ? AND source = ?", node.Id, 100, "provider_import").First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.ProviderBillableBytes != 44 {
		t.Fatalf("updated cycle=%+v", got)
	}

	invalid := []NodeTrafficCycleInput{
		{NodeId: node.Id, CycleStart: 1, Source: "unknown"},
		{NodeId: node.Id, CycleStart: 1, Source: "heartbeat", IngressBytes: -1},
		{NodeId: 999999, CycleStart: 1, Source: "heartbeat"},
	}
	for _, bad := range invalid {
		if err := UpsertNodeTrafficCycle(db, bad); err == nil {
			t.Fatalf("invalid cycle accepted: %+v", bad)
		}
	}
}

func TestListNodeTrafficCyclesScopesPhysicalNodeAndTimeWindow(t *testing.T) {
	setupConflictDB(t)
	db := database.GetDB()
	for i, nodeID := range []int{21, 22} {
		if err := db.Create(&model.Node{Id: nodeID, Name: "billing-cycle-node-" + strconv.Itoa(i), Address: "example.com", Port: 443, ApiToken: "tok"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []model.NodeTrafficCycle{
		{NodeId: 21, CycleStart: 100, CycleEnd: 200, Source: "heartbeat"},
		{NodeId: 21, CycleStart: 200, CycleEnd: 300, Source: "provider_import"},
		{NodeId: 22, CycleStart: 200, CycleEnd: 300, Source: "provider_import"},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	cycles, err := ListNodeTrafficCycles(db, 21, 150, 250)
	if err != nil {
		t.Fatal(err)
	}
	if len(cycles) != 1 || cycles[0].NodeId != 21 || cycles[0].CycleStart != 200 {
		t.Fatalf("cycles=%+v", cycles)
	}
	for i := int64(0); i < 510; i++ {
		if err := db.Create(&model.NodeTrafficCycle{NodeId: 21, CycleStart: 1_000 + i, CycleEnd: 2_000 + i, Source: "heartbeat"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	limited, err := ListNodeTrafficCycles(db, 21, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != maxBillingCycleRows {
		t.Fatalf("bounded cycles=%d, want %d", len(limited), maxBillingCycleRows)
	}
}
