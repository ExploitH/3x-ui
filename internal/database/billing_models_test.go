package database

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestBillingModelsAutoMigrateCreatesTablesAndColumns(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	migrator := GetDB().Migrator()
	checks := []struct {
		name string
		obj  any
		cols []string
	}{
		{
			"node_cost_profiles", &model.NodeCostProfile{},
			[]string{"node_id", "provider", "amount_minor", "currency", "billing_cycle", "included_traffic_bytes", "traffic_calc_type", "overage_price_minor_per_gb", "effective_from", "effective_to"},
		},
		{
			"infrastructure_costs", &model.InfrastructureCost{},
			[]string{"type", "name", "amount_minor", "currency", "billing_cycle", "allocation_mode", "node_id", "path_id", "metadata_json", "effective_from", "effective_to"},
		},
		{
			"node_traffic_cycles", &model.NodeTrafficCycle{},
			[]string{"node_id", "cycle_start", "cycle_end", "ingress_bytes", "egress_bytes", "provider_billable_bytes", "source", "traffic_calc_type"},
		},
		{
			"billing_fx_rates", &model.BillingFxRate{},
			[]string{"base_currency", "quote_currency", "rate_micros", "observed_at", "source"},
		},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if !migrator.HasTable(check.obj) {
				t.Fatalf("table %s was not created", check.name)
			}
			for _, col := range check.cols {
				if !migrator.HasColumn(check.obj, col) {
					t.Errorf("table %s missing column %s", check.name, col)
				}
			}
		})
	}
}

func TestBillingModelsCanPersistMinorUnitsAndPhysicalCycleSources(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	db := GetDB()
	profile := model.NodeCostProfile{
		NodeId: 7, Provider: "AkileCloud", AmountMinor: 999, Currency: "CNY",
		BillingCycle: "monthly", IncludedTrafficBytes: 1000 * 1024 * 1024 * 1024,
		TrafficCalcType: "total", OveragePriceMinorPerGB: 10, EffectiveFrom: 100,
	}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatalf("create node cost profile: %v", err)
	}
	cycle := model.NodeTrafficCycle{
		NodeId: 7, CycleStart: 100, CycleEnd: 200, IngressBytes: 11,
		EgressBytes: 22, ProviderBillableBytes: 33, Source: "provider_import", TrafficCalcType: "total",
	}
	if err := db.Create(&cycle).Error; err != nil {
		t.Fatalf("create traffic cycle: %v", err)
	}
	var got model.NodeCostProfile
	if err := db.First(&got, profile.Id).Error; err != nil {
		t.Fatal(err)
	}
	if got.AmountMinor != 999 || got.Currency != "CNY" || got.IncludedTrafficBytes == 0 {
		t.Fatalf("persisted profile = %+v", got)
	}
	var gotCycle model.NodeTrafficCycle
	if err := db.First(&gotCycle, cycle.Id).Error; err != nil {
		t.Fatal(err)
	}
	if gotCycle.ProviderBillableBytes != 33 || gotCycle.Source != "provider_import" {
		t.Fatalf("persisted cycle = %+v", gotCycle)
	}
}
