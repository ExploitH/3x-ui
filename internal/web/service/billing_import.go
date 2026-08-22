package service

import (
	"errors"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxBillingCycleRows = 500

type NodeTrafficCycleInput struct {
	NodeId                int    `json:"nodeId"`
	CycleStart            int64  `json:"cycleStart"`
	CycleEnd              int64  `json:"cycleEnd"`
	IngressBytes          int64  `json:"ingressBytes"`
	EgressBytes           int64  `json:"egressBytes"`
	ProviderBillableBytes int64  `json:"providerBillableBytes"`
	Source                string `json:"source"`
	TrafficCalcType       string `json:"trafficCalcType"`
}

func validateNodeTrafficCycleInput(db *gorm.DB, input NodeTrafficCycleInput) error {
	if input.NodeId <= 0 || input.CycleStart <= 0 {
		return common.NewError("node traffic cycle nodeId and cycleStart must be positive")
	}
	if input.CycleEnd != 0 && input.CycleEnd <= input.CycleStart {
		return common.NewError("node traffic cycle cycleEnd must be after cycleStart")
	}
	if input.IngressBytes < 0 || input.EgressBytes < 0 || input.ProviderBillableBytes < 0 {
		return common.NewError("node traffic cycle byte counters must not be negative")
	}
	input.Source = strings.ToLower(strings.TrimSpace(input.Source))
	switch input.Source {
	case "heartbeat", "provider_import":
	default:
		return common.NewError("node traffic cycle source must be heartbeat or provider_import")
	}
	input.TrafficCalcType = strings.ToLower(strings.TrimSpace(input.TrafficCalcType))
	if input.TrafficCalcType == "" {
		input.TrafficCalcType = "total"
	}
	switch input.TrafficCalcType {
	case "total", "ul", "dl", "max":
	default:
		return common.NewError("node traffic cycle trafficCalcType must be total, ul, dl or max")
	}
	if db == nil {
		return errors.New("billing database is nil")
	}
	var count int64
	if err := db.Model(&model.Node{}).Where("id = ?", input.NodeId).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return common.NewError("node traffic cycle node does not exist")
	}
	return nil
}

// UpsertNodeTrafficCycle imports one redacted physical/provider snapshot. It is
// idempotent by (node, cycle start, source); no provider API is called here.
func UpsertNodeTrafficCycle(db *gorm.DB, input NodeTrafficCycleInput) error {
	if err := validateNodeTrafficCycleInput(db, input); err != nil {
		return err
	}
	input.Source = strings.ToLower(strings.TrimSpace(input.Source))
	input.TrafficCalcType = strings.ToLower(strings.TrimSpace(input.TrafficCalcType))
	if input.TrafficCalcType == "" {
		input.TrafficCalcType = "total"
	}
	row := model.NodeTrafficCycle{
		NodeId: input.NodeId, CycleStart: input.CycleStart, CycleEnd: input.CycleEnd,
		IngressBytes: input.IngressBytes, EgressBytes: input.EgressBytes,
		ProviderBillableBytes: input.ProviderBillableBytes, Source: input.Source,
		TrafficCalcType: input.TrafficCalcType,
	}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "node_id"}, {Name: "cycle_start"}, {Name: "source"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"cycle_end", "ingress_bytes", "egress_bytes", "provider_billable_bytes", "traffic_calc_type", "updated_at",
		}),
	}).Create(&row).Error
}

func GetNodeTrafficCycle(db *gorm.DB, nodeID int, cycleStart int64, source string) (*model.NodeTrafficCycle, error) {
	if db == nil {
		return nil, errors.New("billing database is nil")
	}
	if nodeID <= 0 || cycleStart <= 0 {
		return nil, common.NewError("nodeId and cycleStart must be positive")
	}
	source = strings.ToLower(strings.TrimSpace(source))
	if source != "heartbeat" && source != "provider_import" {
		return nil, common.NewError("invalid traffic cycle source")
	}
	var row model.NodeTrafficCycle
	if err := db.Where("node_id = ? AND cycle_start = ? AND source = ?", nodeID, cycleStart, source).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func ListNodeTrafficCycles(db *gorm.DB, nodeID int, from, to int64) ([]model.NodeTrafficCycle, error) {
	if db == nil {
		return nil, errors.New("billing database is nil")
	}
	if nodeID <= 0 {
		return nil, common.NewError("nodeId must be positive")
	}
	query := db.Where("node_id = ?", nodeID)
	if from > 0 {
		query = query.Where("cycle_start >= ?", from)
	}
	if to > 0 {
		query = query.Where("cycle_start < ?", to)
	}
	var rows []model.NodeTrafficCycle
	if err := query.Order("cycle_start DESC, id DESC").Limit(maxBillingCycleRows).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
