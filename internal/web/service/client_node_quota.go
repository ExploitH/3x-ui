package service

import (
	"errors"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// addClientNodeUsageDeltaTx advances one physical node's quota-cycle usage for
// a canonical client. NodeClientTraffic remains the raw remote baseline; this
// table stores only non-negative deltas beyond that baseline.
func addClientNodeUsageDeltaTx(tx *gorm.DB, nodeID int, email string, up, down, now int64) error {
	if tx == nil || nodeID <= 0 || email == "" {
		return nil
	}
	if up < 0 {
		up = 0
	}
	if down < 0 {
		down = 0
	}
	if up == 0 && down == 0 {
		return nil
	}

	var client struct {
		Id int
	}
	if err := tx.Model(&model.ClientRecord{}).
		Select("id").
		Where("email = ?", email).
		Take(&client).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// A remote snapshot can briefly precede ClientRecord adoption. Its raw
			// baseline still advances; quota usage begins once the canonical client
			// exists, avoiding creation of an orphan usage row.
			return nil
		}
		return err
	}

	row := model.ClientNodeUsage{
		ClientId:       client.Id,
		NodeId:         nodeID,
		Up:             up,
		Down:           down,
		CycleStartedAt: now,
		UpdatedAt:      now,
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "client_id"}, {Name: "node_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"up":         gorm.Expr(database.ClampedAddExpr("up"), up),
			"down":       gorm.Expr(database.ClampedAddExpr("down"), down),
			"updated_at": now,
		}),
	}).Create(&row).Error; err != nil {
		return err
	}
	return markNodeQuotaExhaustedTx(tx, client.Id, nodeID, now)
}

func markNodeQuotaExhaustedTx(tx *gorm.DB, clientID, nodeID int, now int64) error {
	var quota model.ClientNodeQuota
	if err := tx.Where("client_id = ? AND node_id = ?", clientID, nodeID).Take(&quota).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if quota.TotalBytes <= 0 {
		return nil
	}

	var usage model.ClientNodeUsage
	if err := tx.Where("client_id = ? AND node_id = ?", clientID, nodeID).Take(&usage).Error; err != nil {
		return err
	}
	// Compare without adding Up+Down so even two clamped near-int64 counters
	// cannot overflow and accidentally look below quota.
	exhausted := usage.Up >= quota.TotalBytes ||
		usage.Down >= quota.TotalBytes ||
		usage.Down >= quota.TotalBytes-usage.Up
	if !exhausted {
		return nil
	}

	var state model.ClientNodeAccessState
	err := tx.Where("client_id = ? AND node_id = ?", clientID, nodeID).Take(&state).Error
	if err == nil {
		if state.Blocked && state.Reason == model.ClientNodeAccessReasonQuotaExhausted {
			return nil
		}
		return tx.Model(&state).Updates(map[string]any{
			"blocked":    true,
			"reason":     model.ClientNodeAccessReasonQuotaExhausted,
			"blocked_at": now,
			"applied_at": 0,
			"last_error": "",
			"updated_at": now,
		}).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return tx.Create(&model.ClientNodeAccessState{
		ClientId:  clientID,
		NodeId:    nodeID,
		Blocked:   true,
		Reason:    model.ClientNodeAccessReasonQuotaExhausted,
		BlockedAt: now,
		AppliedAt: 0,
		UpdatedAt: now,
	}).Error
}
