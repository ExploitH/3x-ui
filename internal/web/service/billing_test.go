package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
)

func cleanupBillingTestRows(t *testing.T) {
	t.Helper()
	db := database.GetDB()
	for _, table := range []any{
		&model.NodeCostProfile{}, &model.InfrastructureCost{}, &model.NodeTrafficCycle{}, &model.BillingFxRate{},
	} {
		if err := db.Session(&gorm.Session{AllowGlobalUpdate: true}).Where("1 = 1").Delete(table).Error; err != nil {
			t.Fatalf("cleanup billing rows: %v", err)
		}
	}
}

func TestCalculateMonthlyBillingSeparatesCurrenciesAndAmortizesAnnualCosts(t *testing.T) {
	setupConflictDB(t)
	t.Cleanup(func() { cleanupBillingTestRows(t) })
	db := database.GetDB()
	const asOf int64 = 2_000

	if err := db.Create(&model.NodeCostProfile{
		NodeId: 1, Provider: "Akile", AmountMinor: 999, Currency: "CNY", BillingCycle: "monthly",
		IncludedTrafficBytes: 1000 * billingGiB, TrafficCalcType: "total",
		OveragePriceMinorPerGB: 10, EffectiveFrom: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.NodeTrafficCycle{
		NodeId: 1, CycleStart: 1_000, CycleEnd: 3_000,
		IngressBytes: 4 * billingGiB, EgressBytes: 5 * billingGiB,
		ProviderBillableBytes: 1_025 * billingGiB, Source: "provider_import", TrafficCalcType: "total",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.InfrastructureCost{
		Type: "domain", Name: "427357.xyz", AmountMinor: 1_200, Currency: "CNY",
		BillingCycle: "annual", AllocationMode: "infrastructure", EffectiveFrom: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.InfrastructureCost{
		Type: "cdt", Name: "Alibaba CDT", AmountMinor: 586, Currency: "CNY",
		BillingCycle: "monthly", AllocationMode: "node", EffectiveFrom: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.InfrastructureCost{
		Type: "vps", Name: "JP2 Relay", AmountMinor: 1_200, Currency: "USD",
		BillingCycle: "annual", AllocationMode: "node", EffectiveFrom: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}

	got, err := CalculateMonthlyBilling(db, asOf)
	if err != nil {
		t.Fatalf("CalculateMonthlyBilling: %v", err)
	}
	cny := got.ByCurrency["CNY"]
	usd := got.ByCurrency["USD"]
	// CNY: VPS 999 + 25 GiB overage * 10 + domain 1200/12 + CDT 586.
	if cny.BaseMinor != 1_685 || cny.OverageMinor != 250 || cny.TotalMinor != 1_935 {
		t.Fatalf("CNY summary = %+v", cny)
	}
	if usd.BaseMinor != 100 || usd.OverageMinor != 0 || usd.TotalMinor != 100 {
		t.Fatalf("USD summary = %+v", usd)
	}
	if got.TotalCurrencyCount() != 2 {
		t.Fatalf("currency count=%d, want 2", got.TotalCurrencyCount())
	}
}

func TestCalculateMonthlyBillingDoesNotTreatHeartbeatAsProviderBilling(t *testing.T) {
	setupConflictDB(t)
	t.Cleanup(func() { cleanupBillingTestRows(t) })
	db := database.GetDB()
	if err := db.Create(&model.NodeCostProfile{
		NodeId: 3, AmountMinor: 100, Currency: "CNY", BillingCycle: "monthly",
		IncludedTrafficBytes: 0, OveragePriceMinorPerGB: 99, EffectiveFrom: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.NodeTrafficCycle{
		NodeId: 3, CycleStart: 1, CycleEnd: 3_000,
		IngressBytes: 1, EgressBytes: 1, ProviderBillableBytes: 100 * billingGiB,
		Source: "heartbeat", TrafficCalcType: "total",
	}).Error; err != nil {
		t.Fatal(err)
	}
	got, err := CalculateMonthlyBilling(db, 2_000)
	if err != nil {
		t.Fatal(err)
	}
	if got.ByCurrency["CNY"].OverageMinor != 0 {
		t.Fatalf("heartbeat was billed as provider traffic: %+v", got.ByCurrency["CNY"])
	}
}

func TestCalculateMonthlyBillingUsesLatestEffectiveProfileAndNeverCopiesProviderBytesToNic(t *testing.T) {
	setupConflictDB(t)
	t.Cleanup(func() { cleanupBillingTestRows(t) })
	db := database.GetDB()
	if err := db.Create(&model.NodeCostProfile{
		NodeId: 2, Provider: "old", AmountMinor: 100, Currency: "CNY", BillingCycle: "monthly", EffectiveFrom: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.NodeCostProfile{
		NodeId: 2, Provider: "new", AmountMinor: 200, Currency: "CNY", BillingCycle: "monthly", EffectiveFrom: 1_500,
		IncludedTrafficBytes: 10 * billingGiB, OveragePriceMinorPerGB: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.NodeTrafficCycle{
		NodeId: 2, CycleStart: 1_500, CycleEnd: 2_500,
		IngressBytes: 999 * billingGiB, EgressBytes: 999 * billingGiB,
		ProviderBillableBytes: 11 * billingGiB, Source: "provider_import", TrafficCalcType: "total",
	}).Error; err != nil {
		t.Fatal(err)
	}
	got, err := CalculateMonthlyBilling(db, 2_000)
	if err != nil {
		t.Fatal(err)
	}
	cny := got.ByCurrency["CNY"]
	if cny.BaseMinor != 200 || cny.OverageMinor != 1 || cny.TotalMinor != 201 {
		t.Fatalf("latest profile summary = %+v", cny)
	}
}
