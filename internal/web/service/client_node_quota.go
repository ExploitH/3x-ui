package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"

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
	return s.applyPendingNodeQuotaMutations(ctx, 0, 0)
}

func (s *InboundService) ApplyPendingNodeQuotaBlocksForNode(ctx context.Context, nodeID int) error {
	if nodeID <= 0 {
		return fmt.Errorf("invalid node id %d", nodeID)
	}
	return s.applyPendingNodeQuotaMutations(ctx, nodeID, 0)
}

func (s *InboundService) applyPendingNodeQuotaMutations(ctx context.Context, onlyNodeID, onlyClientID int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	db := database.GetDB()
	var states []model.ClientNodeAccessState
	query := db.Where("reason = ? AND applied_at = 0", model.ClientNodeAccessReasonQuotaExhausted)
	if onlyNodeID > 0 {
		query = query.Where("node_id = ?", onlyNodeID)
	}
	if onlyClientID > 0 {
		query = query.Where("client_id = ?", onlyClientID)
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
			if persistErr := recordNodeQuotaApplyResult(db, state.Id, state.Blocked, 0, applyErr); persistErr != nil {
				allErrs = append(allErrs, persistErr)
			}
			continue
		}
		if !state.Blocked {
			restoreAllowed, err := clientNodeRestoreAllowed(db, &record)
			if err != nil {
				applyErr := fmt.Errorf("node %d client %q: check global restore state: %w", state.NodeId, record.Email, err)
				allErrs = append(allErrs, applyErr)
				if persistErr := recordNodeQuotaApplyResult(db, state.Id, false, 0, applyErr); persistErr != nil {
					allErrs = append(allErrs, persistErr)
				}
				continue
			}
			if !restoreAllowed {
				if persistErr := recordNodeQuotaApplyResult(db, state.Id, false, time.Now().UnixMilli(), nil); persistErr != nil {
					allErrs = append(allErrs, persistErr)
				}
				continue
			}
		}

		var inbounds []*model.Inbound
		if err := db.Model(&model.Inbound{}).
			Joins("JOIN client_inbounds ON client_inbounds.inbound_id = inbounds.id").
			Where("inbounds.node_id = ? AND client_inbounds.client_id = ?", state.NodeId, state.ClientId).
			Order("inbounds.id ASC").
			Find(&inbounds).Error; err != nil {
			applyErr := fmt.Errorf("node %d client %q: load matching inbounds: %w", state.NodeId, record.Email, err)
			allErrs = append(allErrs, applyErr)
			if persistErr := recordNodeQuotaApplyResult(db, state.Id, state.Blocked, 0, applyErr); persistErr != nil {
				allErrs = append(allErrs, persistErr)
			}
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
			matched := false
			for j := range clients {
				if strings.EqualFold(strings.TrimSpace(clients[j].Email), strings.TrimSpace(record.Email)) {
					matched = true
					copyClient := clients[j]
					if state.Blocked {
						copyClient.Enable = false
						payload = &copyClient
					} else if copyClient.Enable {
						payload = &copyClient
					}
					break
				}
			}
			if !matched {
				stateErrs = append(stateErrs, fmt.Errorf("inbound %q: client %q missing from settings", inbound.Tag, record.Email))
				continue
			}
			// A node restore never overrides a per-inbound/manual disabled flag.
			if payload == nil {
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
				action := "disable"
				if !state.Blocked {
					action = "restore"
				}
				stateErrs = append(stateErrs, fmt.Errorf("inbound %q: %s client: %w", inbound.Tag, action, err))
			}
		}

		applyErr := errors.Join(stateErrs...)
		appliedAt := time.Now().UnixMilli()
		if applyErr != nil {
			appliedAt = 0
			allErrs = append(allErrs, fmt.Errorf("node %d client %q: %w", state.NodeId, record.Email, applyErr))
		}
		if err := recordNodeQuotaApplyResult(db, state.Id, state.Blocked, appliedAt, applyErr); err != nil {
			allErrs = append(allErrs, err)
		}
	}
	return errors.Join(allErrs...)
}

func clientNodeRestoreAllowed(db *gorm.DB, record *model.ClientRecord) (bool, error) {
	if db == nil || record == nil || !record.Enable {
		return false, nil
	}
	var traffic xray.ClientTraffic
	if err := db.Where("email = ?", record.Email).Take(&traffic).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if !traffic.Enable {
		return false, nil
	}
	cond, args := depletedCond(db)
	var invalid int64
	if err := db.Model(&xray.ClientTraffic{}).
		Where("email = ?", record.Email).
		Where(cond, args...).
		Count(&invalid).Error; err != nil {
		return false, err
	}
	return invalid == 0, nil
}

// ResetClientNodeTraffic starts a fresh quota cycle for one client on one
// physical node. Raw NodeClientTraffic baselines are intentionally preserved so
// the next node snapshot adds only traffic produced after this reset.
func (s *InboundService) ResetClientNodeTraffic(ctx context.Context, email string, nodeID int) (bool, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return false, common.NewError("client email is required")
	}
	if nodeID <= 0 {
		return false, common.NewError("invalid node id")
	}

	var clientID int
	needsApply := false
	err := submitTrafficWrite(func() error {
		db := database.GetDB()
		return db.Transaction(func(tx *gorm.DB) error {
			var record model.ClientRecord
			if err := tx.Where("email = ?", email).Take(&record).Error; err != nil {
				return err
			}
			clientID = record.Id
			now := time.Now().UnixMilli()
			if err := tx.Model(&model.ClientNodeUsage{}).
				Where("client_id = ? AND node_id = ?", clientID, nodeID).
				Updates(map[string]any{
					"up":               0,
					"down":             0,
					"cycle_started_at": now,
					"updated_at":       now,
				}).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.ClientNodeQuota{}).
				Where("client_id = ? AND node_id = ?", clientID, nodeID).
				Updates(map[string]any{"last_reset_at": now, "updated_at": now}).Error; err != nil {
				return err
			}

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
				needsApply = state.AppliedAt == 0
				return nil
			}
			if err := tx.Model(&state).Updates(map[string]any{
				"blocked":    false,
				"applied_at": 0,
				"last_error": "",
				"updated_at": now,
			}).Error; err != nil {
				return err
			}
			needsApply = true
			return (&NodeService{}).MarkNodeDirtyTx(tx, nodeID)
		})
	})
	if err != nil {
		return false, err
	}
	if !needsApply {
		return false, nil
	}
	if err := s.applyPendingNodeQuotaMutations(ctx, nodeID, clientID); err != nil {
		logger.Warningf("ResetClientNodeTraffic: node %d client %q restore pending: %v", nodeID, email, err)
		return true, nil
	}
	var pending int64
	if err := database.GetDB().Model(&model.ClientNodeAccessState{}).
		Where("client_id = ? AND node_id = ? AND blocked = ? AND reason = ? AND applied_at = 0",
			clientID, nodeID, false, model.ClientNodeAccessReasonQuotaExhausted).
		Count(&pending).Error; err != nil {
		return false, err
	}
	return pending > 0, nil
}

// ResetAllClientNodeTraffic starts a fresh node-quota cycle on every physical
// node configured for one client. Global ClientTraffic counters and raw node
// baselines are deliberately untouched.
func (s *InboundService) ResetAllClientNodeTraffic(ctx context.Context, email string) ([]int, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, common.NewError("client email is required")
	}

	var clientID int
	nodeSet := map[int]struct{}{}
	err := submitTrafficWrite(func() error {
		db := database.GetDB()
		return db.Transaction(func(tx *gorm.DB) error {
			var record model.ClientRecord
			if err := tx.Where("email = ?", email).Take(&record).Error; err != nil {
				return err
			}
			clientID = record.Id
			now := time.Now().UnixMilli()
			if err := tx.Model(&model.ClientNodeUsage{}).
				Where("client_id = ?", clientID).
				Updates(map[string]any{
					"up":               0,
					"down":             0,
					"cycle_started_at": now,
					"updated_at":       now,
				}).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.ClientNodeQuota{}).
				Where("client_id = ?", clientID).
				Updates(map[string]any{"last_reset_at": now, "updated_at": now}).Error; err != nil {
				return err
			}

			var states []model.ClientNodeAccessState
			if err := tx.Where("client_id = ? AND reason = ?", clientID, model.ClientNodeAccessReasonQuotaExhausted).
				Order("node_id ASC").Find(&states).Error; err != nil {
				return err
			}
			for i := range states {
				state := &states[i]
				if !state.Blocked {
					if state.AppliedAt == 0 {
						nodeSet[state.NodeId] = struct{}{}
					}
					continue
				}
				if err := tx.Model(state).Updates(map[string]any{
					"blocked":    false,
					"applied_at": 0,
					"last_error": "",
					"updated_at": now,
				}).Error; err != nil {
					return err
				}
				if err := (&NodeService{}).MarkNodeDirtyTx(tx, state.NodeId); err != nil {
					return err
				}
				nodeSet[state.NodeId] = struct{}{}
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	nodeIDs := make([]int, 0, len(nodeSet))
	for nodeID := range nodeSet {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Ints(nodeIDs)
	for _, nodeID := range nodeIDs {
		if err := s.applyPendingNodeQuotaMutations(ctx, nodeID, clientID); err != nil {
			logger.Warningf("ResetAllClientNodeTraffic: node %d client %q restore pending: %v", nodeID, email, err)
		}
	}

	var pending []int
	if err := database.GetDB().Model(&model.ClientNodeAccessState{}).
		Distinct("node_id").
		Where("client_id = ? AND blocked = ? AND reason = ? AND applied_at = 0",
			clientID, false, model.ClientNodeAccessReasonQuotaExhausted).
		Pluck("node_id", &pending).Error; err != nil {
		return nil, err
	}
	sort.Ints(pending)
	return pending, nil
}

type clientNodeQuotaResetCandidate struct {
	ClientId  int    `gorm:"column:client_id"`
	NodeId    int    `gorm:"column:node_id"`
	ResetDay  int    `gorm:"column:reset_day"`
	LastReset int64  `gorm:"column:last_reset_at"`
	Email     string `gorm:"column:email"`
}

func nodeQuotaResetDue(policy string, resetDay int, lastResetAt int64, now time.Time) bool {
	if now.IsZero() {
		now = time.Now()
	}
	loc := now.Location()
	var cycleStart time.Time
	switch policy {
	case "hourly":
		cycleStart = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, loc)
	case "daily":
		cycleStart = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	case "weekly":
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		daysSinceSunday := int(now.Weekday())
		cycleStart = midnight.AddDate(0, 0, -daysSinceSunday)
	case "monthly":
		if resetDay < 1 {
			resetDay = 1
		}
		lastDay := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, loc).Day()
		scheduledDay := min(resetDay, lastDay)
		if now.Day() != scheduledDay {
			return false
		}
		cycleStart = time.Date(now.Year(), now.Month(), scheduledDay, 0, 0, 0, 0, loc)
	case "never":
		return false
	default:
		return false
	}
	if lastResetAt <= 0 {
		return true
	}
	return time.UnixMilli(lastResetAt).In(loc).Before(cycleStart)
}

// ResetClientNodeQuotasOnSchedule resets every due per-node quota for one cron
// period. Each candidate uses the same reason-aware reset path as the manual API.
func (s *InboundService) ResetClientNodeQuotasOnSchedule(ctx context.Context, policy string, now time.Time) (int, error) {
	policy = strings.ToLower(strings.TrimSpace(policy))
	switch policy {
	case "hourly", "daily", "weekly", "monthly":
	case "never":
		return 0, nil
	default:
		return 0, common.NewError("invalid node quota reset policy")
	}

	var candidates []clientNodeQuotaResetCandidate
	if err := database.GetDB().Table("client_node_quotas AS quota").
		Select("quota.client_id, quota.node_id, quota.reset_day, quota.last_reset_at, clients.email").
		Joins("JOIN clients ON clients.id = quota.client_id").
		Where("quota.reset_policy = ?", policy).
		Order("quota.node_id ASC, quota.client_id ASC").
		Scan(&candidates).Error; err != nil {
		return 0, err
	}

	resetCount := 0
	var resetErrs []error
	for _, candidate := range candidates {
		if !nodeQuotaResetDue(policy, candidate.ResetDay, candidate.LastReset, now) {
			continue
		}
		if _, err := s.ResetClientNodeTraffic(ctx, candidate.Email, candidate.NodeId); err != nil {
			resetErrs = append(resetErrs, fmt.Errorf("node %d client %q: %w", candidate.NodeId, candidate.Email, err))
			continue
		}
		resetCount++
	}
	return resetCount, errors.Join(resetErrs...)
}

func recordNodeQuotaApplyResult(db *gorm.DB, stateID int, blocked bool, appliedAt int64, applyErr error) error {
	message := ""
	if applyErr != nil {
		message = applyErr.Error()
		const maxStoredError = 2048
		if len(message) > maxStoredError {
			message = message[:maxStoredError]
		}
	}
	return db.Model(&model.ClientNodeAccessState{}).
		Where("id = ? AND blocked = ? AND reason = ?", stateID, blocked, model.ClientNodeAccessReasonQuotaExhausted).
		Updates(map[string]any{
			"applied_at": appliedAt,
			"last_error": message,
			"updated_at": time.Now().UnixMilli(),
		}).Error
}
