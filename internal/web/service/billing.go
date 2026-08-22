package service

import (
	"errors"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
)

const billingGiB int64 = 1 << 30

type BillingCurrencySummary struct {
	BaseMinor    int64 `json:"baseMinor"`
	OverageMinor int64 `json:"overageMinor"`
	TotalMinor   int64 `json:"totalMinor"`
}

type BillingLine struct {
	Category     string `json:"category"`
	Name         string `json:"name"`
	Currency     string `json:"currency"`
	BaseMinor    int64  `json:"baseMinor"`
	OverageMinor int64  `json:"overageMinor"`
	TotalMinor   int64  `json:"totalMinor"`
	Source       string `json:"source"`
}

type MonthlyBillingSummary struct {
	AsOf       int64                             `json:"asOf"`
	ByCurrency map[string]BillingCurrencySummary `json:"byCurrency"`
	Lines      []BillingLine                     `json:"lines"`
}

func (s MonthlyBillingSummary) TotalCurrencyCount() int { return len(s.ByCurrency) }

func addBillingLine(summary *MonthlyBillingSummary, line BillingLine) {
	if summary.ByCurrency == nil {
		summary.ByCurrency = make(map[string]BillingCurrencySummary)
	}
	line.TotalMinor = line.BaseMinor + line.OverageMinor
	current := summary.ByCurrency[line.Currency]
	current.BaseMinor += line.BaseMinor
	current.OverageMinor += line.OverageMinor
	current.TotalMinor += line.TotalMinor
	summary.ByCurrency[line.Currency] = current
	summary.Lines = append(summary.Lines, line)
}

func monthlyAmortizedMinor(amount int64, cycle string) int64 {
	if amount <= 0 {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(cycle)) {
	case "year", "yearly", "annual", "annually":
		return ceilDivide(amount, 12)
	default:
		return amount
	}
}

func ceilDivide(value, divisor int64) int64 {
	if value <= 0 || divisor <= 0 {
		return 0
	}
	result := value / divisor
	if value%divisor != 0 {
		result++
	}
	return result
}

func overageMinor(providerBillableBytes, includedBytes, priceMinorPerGB int64) int64 {
	if providerBillableBytes <= 0 || includedBytes < 0 || providerBillableBytes <= includedBytes || priceMinorPerGB <= 0 {
		return 0
	}
	excess := new(big.Int).SetInt64(providerBillableBytes - includedBytes)
	price := new(big.Int).SetInt64(priceMinorPerGB)
	numerator := new(big.Int).Mul(excess, price)
	numerator.Add(numerator, big.NewInt(billingGiB-1))
	numerator.Div(numerator, big.NewInt(billingGiB))
	if !numerator.IsInt64() {
		return int64(^uint64(0) >> 1)
	}
	return numerator.Int64()
}

func activeNodeProfiles(db *gorm.DB, asOf int64) (map[int]model.NodeCostProfile, error) {
	var rows []model.NodeCostProfile
	if err := db.Where("effective_from <= ? AND (effective_to = 0 OR effective_to > ?)", asOf, asOf).
		Order("node_id ASC, effective_from DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	selected := make(map[int]model.NodeCostProfile)
	for _, row := range rows {
		if _, exists := selected[row.NodeId]; !exists {
			selected[row.NodeId] = row
		}
	}
	return selected, nil
}

func latestProviderCycle(db *gorm.DB, nodeID int, asOf int64) (*model.NodeTrafficCycle, error) {
	var cycle model.NodeTrafficCycle
	err := db.Where("node_id = ? AND cycle_start <= ? AND (cycle_end = 0 OR cycle_end > ?) AND source = ?", nodeID, asOf, asOf, "provider_import").
		Order("cycle_start DESC, id DESC").First(&cycle).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cycle, nil
}

// CalculateMonthlyBilling produces a currency-separated monthly estimate. It
// deliberately uses provider_billable_bytes from an explicit provider_import
// cycle for overage; NIC ingress/egress and heartbeat-only cycles remain
// reconciliation metrics and are never double-billed.
func CalculateMonthlyBilling(db *gorm.DB, asOf int64) (MonthlyBillingSummary, error) {
	if db == nil {
		return MonthlyBillingSummary{}, errors.New("billing database is nil")
	}
	profiles, err := activeNodeProfiles(db, asOf)
	if err != nil {
		return MonthlyBillingSummary{}, err
	}
	summary := MonthlyBillingSummary{AsOf: asOf, ByCurrency: make(map[string]BillingCurrencySummary)}
	nodeIDs := make([]int, 0, len(profiles))
	for nodeID := range profiles {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Ints(nodeIDs)
	for _, nodeID := range nodeIDs {
		profile := profiles[nodeID]
		base := monthlyAmortizedMinor(profile.AmountMinor, profile.BillingCycle)
		overage := int64(0)
		cycle, cycleErr := latestProviderCycle(db, nodeID, asOf)
		if cycleErr != nil {
			return MonthlyBillingSummary{}, cycleErr
		}
		if cycle != nil {
			overage = overageMinor(cycle.ProviderBillableBytes, profile.IncludedTrafficBytes, profile.OveragePriceMinorPerGB)
		}
		addBillingLine(&summary, BillingLine{
			Category: "vps", Name: "node:" + strconv.Itoa(nodeID), Currency: profile.Currency,
			BaseMinor: base, OverageMinor: overage, Source: profile.Provider,
		})
	}

	var infra []model.InfrastructureCost
	if err := db.Where("effective_from <= ? AND (effective_to = 0 OR effective_to > ?)", asOf, asOf).
		Order("type ASC, name ASC, id ASC").Find(&infra).Error; err != nil {
		return MonthlyBillingSummary{}, err
	}
	for _, cost := range infra {
		addBillingLine(&summary, BillingLine{
			Category: cost.Type, Name: cost.Name, Currency: cost.Currency,
			BaseMinor: monthlyAmortizedMinor(cost.AmountMinor, cost.BillingCycle), Source: "infrastructure_costs",
		})
	}
	return summary, nil
}
