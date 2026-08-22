package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"

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
			"up":         gorm.Expr(database.ClampedAddExpr("client_node_usages.up"), up),
			"down":       gorm.Expr(database.ClampedAddExpr("client_node_usages.down"), down),
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
		if err := tx.Model(&state).Updates(map[string]any{
			"blocked":    true,
			"reason":     model.ClientNodeAccessReasonQuotaExhausted,
			"blocked_at": now,
			"applied_at": 0,
			"last_error": "",
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		return (&NodeService{}).MarkNodeDirtyTx(tx, nodeID)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err := tx.Create(&model.ClientNodeAccessState{
		ClientId:  clientID,
		NodeId:    nodeID,
		Blocked:   true,
		Reason:    model.ClientNodeAccessReasonQuotaExhausted,
		BlockedAt: now,
		AppliedAt: 0,
		UpdatedAt: now,
	}).Error; err != nil {
		return err
	}
	return (&NodeService{}).MarkNodeDirtyTx(tx, nodeID)
}

type nodeQuotaBlockedClient struct {
	Email  string
	Enable bool
}

func blockedNodeClientDesired(tx *gorm.DB, nodeID int) (map[string]bool, error) {
	out := map[string]bool{}
	if tx == nil || nodeID <= 0 {
		return out, nil
	}
	var rows []nodeQuotaBlockedClient
	if err := tx.Table("client_node_access_states AS access").
		Select("clients.email AS email, clients.enable AS enable").
		Joins("JOIN clients ON clients.id = access.client_id").
		Where("access.node_id = ? AND access.blocked = ? AND access.reason = ?",
			nodeID, true, model.ClientNodeAccessReasonQuotaExhausted).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if email := strings.ToLower(strings.TrimSpace(row.Email)); email != "" {
			out[email] = row.Enable
		}
	}
	return out, nil
}

func nodeQuotaBlockOverrides(tx *gorm.DB, nodeID int) (map[string]bool, error) {
	blocked, err := blockedNodeClientDesired(tx, nodeID)
	if err != nil {
		return nil, err
	}
	for email := range blocked {
		blocked[email] = false
	}
	return blocked, nil
}

func rewriteClientEnableMap(settings map[string]any, overrides map[string]bool) bool {
	if len(overrides) == 0 {
		return false
	}
	clients, ok := settings["clients"].([]any)
	if !ok {
		return false
	}
	changed := false
	for _, raw := range clients {
		client, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		email, _ := client["email"].(string)
		want, ok := overrides[strings.ToLower(strings.TrimSpace(email))]
		if !ok {
			continue
		}
		if got, exists := client["enable"].(bool); !exists || got != want {
			client["enable"] = want
			changed = true
		}
	}
	return changed
}

func rewriteClientEnableSettings(raw string, overrides map[string]bool) (string, bool, error) {
	if len(overrides) == 0 {
		return raw, false, nil
	}
	settings := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return raw, false, err
	}
	if !rewriteClientEnableMap(settings, overrides) {
		return raw, false, nil
	}
	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return raw, false, err
	}
	return string(encoded), true, nil
}

func desiredClientEnableFromSettings(raw string, blocked map[string]bool) (map[string]bool, error) {
	out := make(map[string]bool, len(blocked))
	if len(blocked) == 0 {
		return out, nil
	}
	settings := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return nil, err
	}
	clients, ok := settings["clients"].([]any)
	if !ok {
		return out, nil
	}
	for _, rawClient := range clients {
		client, ok := rawClient.(map[string]any)
		if !ok {
			continue
		}
		email, _ := client["email"].(string)
		key := strings.ToLower(strings.TrimSpace(email))
		if _, blockedHere := blocked[key]; !blockedHere {
			continue
		}
		enabled := true
		if explicit, ok := client["enable"].(bool); ok {
			enabled = explicit
		}
		out[key] = enabled
	}
	return out, nil
}

// restoreNodeQuotaSnapshotDesiredState removes the node-only enable overlay
// before a node snapshot is merged back into the master's canonical settings
// and shared ClientTraffic row. Counters remain untouched.
func restoreNodeQuotaSnapshotDesiredState(
	tx *gorm.DB,
	nodeID int,
	snap *runtime.TrafficSnapshot,
	tagToCentral map[string]*model.Inbound,
	globalDesired map[string]bool,
) error {
	if snap == nil {
		return nil
	}
	blocked, err := blockedNodeClientDesired(tx, nodeID)
	if err != nil || len(blocked) == 0 {
		return err
	}
	for _, snapInbound := range snap.Inbounds {
		if snapInbound == nil {
			continue
		}
		settingsDesired := make(map[string]bool, len(blocked))
		for email, enabled := range blocked {
			settingsDesired[email] = enabled
		}
		if central := tagToCentral[snapInbound.Tag]; central != nil {
			centralDesired, derr := desiredClientEnableFromSettings(central.Settings, blocked)
			if derr != nil {
				return derr
			}
			for email, enabled := range centralDesired {
				settingsDesired[email] = enabled
			}
		}
		restored, changed, rerr := rewriteClientEnableSettings(snapInbound.Settings, settingsDesired)
		if rerr != nil {
			return rerr
		}
		if changed {
			snapInbound.Settings = restored
		}
		for i := range snapInbound.ClientStats {
			key := strings.ToLower(strings.TrimSpace(snapInbound.ClientStats[i].Email))
			fallback, blockedHere := blocked[key]
			if !blockedHere {
				continue
			}
			if enabled, ok := globalDesired[key]; ok {
				snapInbound.ClientStats[i].Enable = enabled
			} else {
				snapInbound.ClientStats[i].Enable = fallback
			}
		}
	}
	return nil
}

// ApplyPendingNodeQuotaBlocks applies node-local quota state only to inbounds
// attached to the matching physical node. It deliberately performs network I/O
// after the accounting transaction and never mutates canonical/global enable.
func (s *InboundService) ApplyPendingNodeQuotaBlocks(ctx context.Context) error {
	return s.applyPendingNodeQuotaBlocks(ctx, 0)
}

func (s *InboundService) ApplyPendingNodeQuotaBlocksForNode(ctx context.Context, nodeID int) error {
	if nodeID <= 0 {
		return fmt.Errorf("invalid node id %d", nodeID)
	}
	return s.applyPendingNodeQuotaBlocks(ctx, nodeID)
}

func (s *InboundService) applyPendingNodeQuotaBlocks(ctx context.Context, onlyNodeID int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	db := database.GetDB()
	var states []model.ClientNodeAccessState
	query := db.Where("blocked = ? AND reason = ? AND applied_at = 0",
		true, model.ClientNodeAccessReasonQuotaExhausted)
	if onlyNodeID > 0 {
		query = query.Where("node_id = ?", onlyNodeID)
	}
	if err := query.Order("node_id ASC, client_id ASC").Find(&states).Error; err != nil {
		return err
	}

	var allErrs []error
	for i := range states {
		state := &states[i]
		var record model.ClientRecord
		if err := db.Where("id = ?", state.ClientId).Take(&record).Error; err != nil {
			applyErr := fmt.Errorf("node %d client %d: load canonical client: %w", state.NodeId, state.ClientId, err)
			allErrs = append(allErrs, applyErr)
			_ = recordNodeQuotaApplyResult(db, state.Id, 0, applyErr)
			continue
		}

		var inbounds []*model.Inbound
		if err := db.Model(&model.Inbound{}).
			Joins("JOIN client_inbounds ON client_inbounds.inbound_id = inbounds.id").
			Where("inbounds.node_id = ? AND client_inbounds.client_id = ?", state.NodeId, state.ClientId).
			Order("inbounds.id ASC").
			Find(&inbounds).Error; err != nil {
			applyErr := fmt.Errorf("node %d client %q: load matching inbounds: %w", state.NodeId, record.Email, err)
			allErrs = append(allErrs, applyErr)
			_ = recordNodeQuotaApplyResult(db, state.Id, 0, applyErr)
			continue
		}

		var stateErrs []error
		for _, inbound := range inbounds {
			clients, err := s.GetClients(inbound)
			if err != nil {
				stateErrs = append(stateErrs, fmt.Errorf("inbound %q: decode clients: %w", inbound.Tag, err))
				continue
			}
			var payload *model.Client
			for j := range clients {
				if strings.EqualFold(strings.TrimSpace(clients[j].Email), strings.TrimSpace(record.Email)) {
					copyClient := clients[j]
					copyClient.Enable = false
					payload = &copyClient
					break
				}
			}
			if payload == nil {
				stateErrs = append(stateErrs, fmt.Errorf("inbound %q: client %q missing from settings", inbound.Tag, record.Email))
				continue
			}
			rt, push, pending, err := s.nodePushPlan(inbound)
			if err != nil {
				stateErrs = append(stateErrs, fmt.Errorf("inbound %q: resolve runtime: %w", inbound.Tag, err))
				continue
			}
			if !push {
				reason := "runtime unavailable"
				if pending {
					reason = "node offline or disabled"
				}
				stateErrs = append(stateErrs, fmt.Errorf("inbound %q: %s", inbound.Tag, reason))
				continue
			}
			if err := rt.UpdateUser(ctx, inbound, record.Email, *payload); err != nil {
				stateErrs = append(stateErrs, fmt.Errorf("inbound %q: disable client: %w", inbound.Tag, err))
			}
		}

		applyErr := errors.Join(stateErrs...)
		appliedAt := time.Now().UnixMilli()
		if applyErr != nil {
			appliedAt = 0
			allErrs = append(allErrs, fmt.Errorf("node %d client %q: %w", state.NodeId, record.Email, applyErr))
		}
		if err := recordNodeQuotaApplyResult(db, state.Id, appliedAt, applyErr); err != nil {
			allErrs = append(allErrs, err)
		}
	}
	return errors.Join(allErrs...)
}

func recordNodeQuotaApplyResult(db *gorm.DB, stateID int, appliedAt int64, applyErr error) error {
	message := ""
	if applyErr != nil {
		message = applyErr.Error()
		const maxStoredError = 2048
		if len(message) > maxStoredError {
			message = message[:maxStoredError]
		}
	}
	return db.Model(&model.ClientNodeAccessState{}).
		Where("id = ? AND blocked = ? AND reason = ?", stateID, true, model.ClientNodeAccessReasonQuotaExhausted).
		Updates(map[string]any{
			"applied_at": appliedAt,
			"last_error": message,
			"updated_at": time.Now().UnixMilli(),
		}).Error
}
