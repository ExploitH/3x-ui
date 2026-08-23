package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

// ApplyPendingNodeQuotaBlocksForAccountingNode applies quota mutations when a
// disabled physical node has no central inbounds rows. The remote's inventory
// supplies the actual inbound tags, while the normal quota state machine remains
// the source of blocked/admin/reset truth.
func (s *InboundService) ApplyPendingNodeQuotaBlocksForAccountingNode(ctx context.Context, nodeID int, remote *runtime.Remote) error {
	if nodeID <= 0 || remote == nil {
		return fmt.Errorf("accounting-node quota mutation requires node and remote")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	options, err := remote.ListInboundOptions(ctx)
	if err != nil {
		return err
	}
	if len(options) == 0 {
		return errors.New("accounting node exposes no inbounds")
	}
	db := database.GetDB()
	var states []model.ClientNodeAccessState
	if err := db.Where("node_id = ? AND reason = ? AND applied_at = 0", nodeID, model.ClientNodeAccessReasonQuotaExhausted).
		Order("client_id ASC").Find(&states).Error; err != nil {
		return err
	}
	var allErrs []error
	for i := range states {
		state := &states[i]
		var record model.ClientRecord
		if err := db.Where("id = ?", state.ClientId).Take(&record).Error; err != nil {
			applyErr := fmt.Errorf("node %d client %d: load canonical client: %w", nodeID, state.ClientId, err)
			allErrs = append(allErrs, applyErr)
			_ = recordNodeQuotaApplyResult(db, state.Id, state.Blocked, 0, applyErr)
			continue
		}
		if !state.Blocked {
			allowed, err := clientNodeRestoreAllowed(db, &record)
			if err != nil {
				applyErr := fmt.Errorf("node %d client %q: check restore state: %w", nodeID, record.Email, err)
				allErrs = append(allErrs, applyErr)
				_ = recordNodeQuotaApplyResult(db, state.Id, false, 0, applyErr)
				continue
			}
			if !allowed {
				_ = recordNodeQuotaApplyResult(db, state.Id, false, time.Now().UnixMilli(), nil)
				continue
			}
		}
		mutationCtx := ctx
		if state.Blocked {
			mutationCtx = runtime.WithNodeMutationReason(ctx, runtime.MutationReasonQuotaBlock)
		} else {
			mutationCtx = runtime.WithNodeMutationReason(ctx, runtime.MutationReasonQuotaReset)
		}
		payload := model.Client{Email: record.Email, Enable: !state.Blocked}
		var applyErrs []error
		applied := false
		// A direct adapter scopes the mutation to its physical-node direct tags;
		// stop after the first successful tag so one quota event does not restart
		// the same sing-box service once per protocol.
		for _, option := range options {
			if err := remote.UpdateUser(mutationCtx, &model.Inbound{Id: option.Id, Tag: option.Tag}, record.Email, payload); err != nil {
				applyErrs = append(applyErrs, fmt.Errorf("inbound %q: %w", option.Tag, err))
				continue
			}
			applied = true
			break
		}
		var applyErr error
		if !applied {
			applyErr = errors.Join(applyErrs...)
			if applyErr == nil {
				applyErr = errors.New("no accounting-node inbound mutation succeeded")
			}
			allErrs = append(allErrs, fmt.Errorf("node %d client %q: %w", nodeID, record.Email, applyErr))
		}
		appliedAt := time.Now().UnixMilli()
		if applyErr != nil {
			appliedAt = 0
		}
		if err := recordNodeQuotaApplyResult(db, state.Id, state.Blocked, appliedAt, applyErr); err != nil {
			allErrs = append(allErrs, err)
		}
	}
	return errors.Join(allErrs...)
}

func accountingNodeOptionIDs(options []runtime.RemoteInboundOption) []int {
	ids := make([]int, 0, len(options))
	for _, option := range options {
		ids = append(ids, option.Id)
	}
	sort.Ints(ids)
	return ids
}
