package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"

	"gorm.io/gorm"
)

// ApplySingboxTrafficSnapshot applies a validated cumulative snapshot for one
// physical node. It is intentionally not called by NodeTrafficSyncJob yet.
// NodeClientTraffic remains the raw cumulative baseline; ClientNodeUsage receives
// only deltas beyond that baseline in the same transaction.
func (s *InboundService) ApplySingboxTrafficSnapshot(nodeID int, snapshot *runtime.SingboxTrafficSnapshot) (bool, error) {
	if nodeID <= 0 {
		return false, fmt.Errorf("invalid node id %d", nodeID)
	}
	if snapshot == nil {
		return false, errors.New("sing-box traffic snapshot is nil")
	}

	var applied bool
	err := submitTrafficWrite(func() error {
		db := database.GetDB()
		var canonicalEmails []string
		if err := db.Model(&model.ClientRecord{}).Pluck("email", &canonicalEmails).Error; err != nil {
			return err
		}
		mapped, err := runtime.MapSingboxTrafficUsers(snapshot, canonicalEmails)
		if err != nil {
			return err
		}
		now := time.Now().UnixMilli()
		return db.Transaction(func(tx *gorm.DB) error {
			for _, user := range mapped {
				var baseline model.NodeClientTraffic
				err := tx.Where("node_id = ? AND email = ?", nodeID, user.Email).Take(&baseline).Error
				if errors.Is(err, gorm.ErrRecordNotFound) {
					if err := (&InboundService{}).upsertNodeBaseline(tx, nodeID, user.Email, user.Uplink, user.Downlink); err != nil {
						return err
					}
					continue
				}
				if err != nil {
					return err
				}

				deltaUp := user.Uplink - baseline.Up
				deltaDown := user.Downlink - baseline.Down
				if deltaUp < 0 {
					deltaUp = user.Uplink
				}
				if deltaDown < 0 {
					deltaDown = user.Downlink
				}
				if err := addClientNodeUsageDeltaTx(tx, nodeID, user.Email, deltaUp, deltaDown, now); err != nil {
					return err
				}
				if err := (&InboundService{}).upsertNodeBaseline(tx, nodeID, user.Email, user.Uplink, user.Downlink); err != nil {
					return err
				}
				if deltaUp > 0 || deltaDown > 0 {
					applied = true
				}
			}
			return nil
		})
	})
	return applied, err
}
