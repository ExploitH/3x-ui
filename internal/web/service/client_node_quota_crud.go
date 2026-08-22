package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ClientNodeQuotaInput struct {
	NodeId      int    `json:"nodeId"`
	TotalBytes  int64  `json:"totalBytes"`
	ResetPolicy string `json:"resetPolicy"`
	ResetDay    int    `json:"resetDay"`
}

type ClientNodeQuotaView struct {
	NodeId       int    `json:"nodeId" gorm:"column:node_id"`
	NodeName     string `json:"nodeName" gorm:"column:node_name"`
	TotalBytes   int64  `json:"totalBytes" gorm:"column:total_bytes"`
	ResetPolicy  string `json:"resetPolicy" gorm:"column:reset_policy"`
	ResetDay     int    `json:"resetDay" gorm:"column:reset_day"`
	LastResetAt  int64  `json:"lastResetAt" gorm:"column:last_reset_at"`
	Up           int64  `json:"up" gorm:"column:up"`
	Down         int64  `json:"down" gorm:"column:down"`
	Blocked      bool   `json:"blocked" gorm:"column:blocked"`
	Reason       string `json:"reason" gorm:"column:reason"`
	BlockedAt    int64  `json:"blockedAt" gorm:"column:blocked_at"`
	AppliedAt    int64  `json:"appliedAt" gorm:"column:applied_at"`
	LastError    string `json:"lastError" gorm:"column:last_error"`
	QuotaUpdated int64  `json:"updatedAt" gorm:"column:quota_updated_at"`
}

func normalizeClientNodeQuotaInputs(inputs []ClientNodeQuotaInput) ([]ClientNodeQuotaInput, error) {
	out := make([]ClientNodeQuotaInput, len(inputs))
	copy(out, inputs)
	seen := make(map[int]struct{}, len(out))
	for i := range out {
		quota := &out[i]
		if quota.NodeId <= 0 {
			return nil, common.NewError("node quota nodeId must be positive")
		}
		if _, exists := seen[quota.NodeId]; exists {
			return nil, common.NewError("duplicate node quota nodeId:", quota.NodeId)
		}
		seen[quota.NodeId] = struct{}{}
		if quota.TotalBytes < 0 {
			return nil, common.NewError("node quota totalBytes must not be negative")
		}
		quota.ResetPolicy = strings.ToLower(strings.TrimSpace(quota.ResetPolicy))
		if quota.ResetPolicy == "" {
			quota.ResetPolicy = "never"
		}
		switch quota.ResetPolicy {
		case "never", "hourly", "daily", "weekly", "monthly":
		default:
			return nil, common.NewError("invalid node quota resetPolicy:", quota.ResetPolicy)
		}
		if quota.ResetDay == 0 {
			quota.ResetDay = 1
		}
		if quota.ResetDay < 1 || quota.ResetDay > 31 {
			return nil, common.NewError("node quota resetDay must be between 1 and 31")
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeId < out[j].NodeId })
	return out, nil
}

func validateClientNodeQuotaNodes(db *gorm.DB, inputs []ClientNodeQuotaInput) error {
	if len(inputs) == 0 {
		return nil
	}
	ids := make([]int, 0, len(inputs))
	for _, input := range inputs {
		ids = append(ids, input.NodeId)
	}
	var found []int
	if err := db.Model(&model.Node{}).Where("id IN ?", ids).Pluck("id", &found).Error; err != nil {
		return err
	}
	if len(found) != len(ids) {
		return common.NewError("one or more node quota nodeIds do not exist")
	}
	return nil
}

func (s *InboundService) GetClientNodeQuotas(email string) ([]ClientNodeQuotaView, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, common.NewError("client email is required")
	}
	var record model.ClientRecord
	db := database.GetDB()
	if err := db.Select("id").Where("email = ?", email).Take(&record).Error; err != nil {
		return nil, err
	}
	views := make([]ClientNodeQuotaView, 0)
	err := db.Table("client_node_quotas AS quota").
		Select(`quota.node_id, nodes.name AS node_name, quota.total_bytes,
			quota.reset_policy, quota.reset_day, quota.last_reset_at,
			COALESCE(usage.up, 0) AS up, COALESCE(usage.down, 0) AS down,
			COALESCE(access.blocked, false) AS blocked,
			COALESCE(access.reason, '') AS reason,
			COALESCE(access.blocked_at, 0) AS blocked_at,
			COALESCE(access.applied_at, 0) AS applied_at,
			COALESCE(access.last_error, '') AS last_error,
			quota.updated_at AS quota_updated_at`).
		Joins("JOIN nodes ON nodes.id = quota.node_id").
		Joins("LEFT JOIN client_node_usages AS usage ON usage.client_id = quota.client_id AND usage.node_id = quota.node_id").
		Joins("LEFT JOIN client_node_access_states AS access ON access.client_id = quota.client_id AND access.node_id = quota.node_id").
		Where("quota.client_id = ?", record.Id).
		Order("nodes.name ASC, quota.node_id ASC").
		Scan(&views).Error
	return views, err
}

func (s *InboundService) ReplaceClientNodeQuotas(ctx context.Context, email string, inputs []ClientNodeQuotaInput) ([]int, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, common.NewError("client email is required")
	}
	normalized, err := normalizeClientNodeQuotaInputs(inputs)
	if err != nil {
		return nil, err
	}
	db := database.GetDB()
	var record model.ClientRecord
	if err := db.Where("email = ?", email).Take(&record).Error; err != nil {
		return nil, err
	}
	if err := validateClientNodeQuotaNodes(db, normalized); err != nil {
		return nil, err
	}

	mutationNodes := map[int]struct{}{}
	err = submitTrafficWrite(func() error {
		return database.GetDB().Transaction(func(tx *gorm.DB) error {
			now := time.Now().UnixMilli()
			var existing []model.ClientNodeQuota
			if err := tx.Where("client_id = ?", record.Id).Find(&existing).Error; err != nil {
				return err
			}
			desired := make(map[int]ClientNodeQuotaInput, len(normalized))
			for _, input := range normalized {
				desired[input.NodeId] = input
				row := model.ClientNodeQuota{
					ClientId: record.Id, NodeId: input.NodeId, TotalBytes: input.TotalBytes,
					ResetPolicy: input.ResetPolicy, ResetDay: input.ResetDay,
				}
				if err := tx.Clauses(clause.OnConflict{
					Columns: []clause.Column{{Name: "client_id"}, {Name: "node_id"}},
					DoUpdates: clause.Assignments(map[string]any{
						"total_bytes": input.TotalBytes, "reset_policy": input.ResetPolicy,
						"reset_day": input.ResetDay, "updated_at": now,
					}),
				}).Create(&row).Error; err != nil {
					return err
				}
				if err := reconcileClientNodeQuotaConfigTx(tx, record.Id, input.NodeId, input.TotalBytes, now, mutationNodes); err != nil {
					return err
				}
			}

			for _, old := range existing {
				if _, keep := desired[old.NodeId]; keep {
					continue
				}
				if err := queueClientNodeQuotaRestoreTx(tx, record.Id, old.NodeId, now, mutationNodes); err != nil {
					return err
				}
				if err := tx.Where("client_id = ? AND node_id = ?", record.Id, old.NodeId).
					Delete(&model.ClientNodeUsage{}).Error; err != nil {
					return err
				}
				if err := tx.Where("client_id = ? AND node_id = ?", record.Id, old.NodeId).
					Delete(&model.ClientNodeQuota{}).Error; err != nil {
					return err
				}
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	nodeIDs := make([]int, 0, len(mutationNodes))
	for nodeID := range mutationNodes {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Ints(nodeIDs)
	for _, nodeID := range nodeIDs {
		if err := s.applyPendingNodeQuotaMutations(ctx, nodeID, record.Id); err != nil {
			logger.Warningf("ReplaceClientNodeQuotas: node %d client %q mutation pending: %v", nodeID, email, err)
		}
	}
	var pending []int
	if err := database.GetDB().Model(&model.ClientNodeAccessState{}).
		Distinct("node_id").
		Where("client_id = ? AND reason = ? AND applied_at = 0", record.Id, model.ClientNodeAccessReasonQuotaExhausted).
		Pluck("node_id", &pending).Error; err != nil {
		return nil, err
	}
	sort.Ints(pending)
	return pending, nil
}

func reconcileClientNodeQuotaConfigTx(tx *gorm.DB, clientID, nodeID int, totalBytes, now int64, mutationNodes map[int]struct{}) error {
	var usage model.ClientNodeUsage
	err := tx.Where("client_id = ? AND node_id = ?", clientID, nodeID).Take(&usage).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	exhausted := false
	if err == nil && totalBytes > 0 {
		exhausted = usage.Up >= totalBytes || usage.Down >= totalBytes || usage.Down >= totalBytes-usage.Up
	}
	if exhausted {
		if err := markNodeQuotaExhaustedTx(tx, clientID, nodeID, now); err != nil {
			return err
		}
		mutationNodes[nodeID] = struct{}{}
		return nil
	}
	return queueClientNodeQuotaRestoreTx(tx, clientID, nodeID, now, mutationNodes)
}

func queueClientNodeQuotaRestoreTx(tx *gorm.DB, clientID, nodeID int, now int64, mutationNodes map[int]struct{}) error {
	var state model.ClientNodeAccessState
	err := tx.Where("client_id = ? AND node_id = ?", clientID, nodeID).Take(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if state.Reason != model.ClientNodeAccessReasonQuotaExhausted {
		return nil
	}
	if !state.Blocked {
		if state.AppliedAt == 0 {
			mutationNodes[nodeID] = struct{}{}
		}
		return nil
	}
	if err := tx.Model(&state).Updates(map[string]any{
		"blocked": false, "applied_at": 0, "last_error": "", "updated_at": now,
	}).Error; err != nil {
		return err
	}
	if err := (&NodeService{}).MarkNodeDirtyTx(tx, nodeID); err != nil {
		return err
	}
	mutationNodes[nodeID] = struct{}{}
	return nil
}

func deleteClientNodeQuotaRowsTx(tx *gorm.DB, clientIDs []int) error {
	if tx == nil || len(clientIDs) == 0 {
		return nil
	}
	for _, batch := range chunkInts(clientIDs, sqlInChunk) {
		if err := tx.Where("client_id IN ?", batch).Delete(&model.ClientNodeAccessState{}).Error; err != nil {
			return err
		}
		if err := tx.Where("client_id IN ?", batch).Delete(&model.ClientNodeUsage{}).Error; err != nil {
			return err
		}
		if err := tx.Where("client_id IN ?", batch).Delete(&model.ClientNodeQuota{}).Error; err != nil {
			return err
		}
	}
	return nil
}

func deleteClientNodeQuotaRowsByEmailsTx(tx *gorm.DB, emails []string) error {
	if tx == nil || len(emails) == 0 {
		return nil
	}
	var ids []int
	if err := tx.Model(&model.ClientRecord{}).Where("email IN ?", emails).Pluck("id", &ids).Error; err != nil {
		return err
	}
	return deleteClientNodeQuotaRowsTx(tx, ids)
}
