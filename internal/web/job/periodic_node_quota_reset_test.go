package job

import (
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type seededNodeQuota struct {
	clientID int
	nodeID   int
}

func seedScheduledNodeQuota(t *testing.T, email, policy string, resetDay int, lastResetAt, up, down int64) seededNodeQuota {
	t.Helper()
	db := database.GetDB()
	record := model.ClientRecord{Email: email, Enable: true}
	if err := db.Create(&record).Error; err != nil {
		t.Fatalf("create client %q: %v", email, err)
	}
	nodeID := 8_000 + record.Id
	if err := db.Create(&model.ClientNodeQuota{
		ClientId: record.Id, NodeId: nodeID, TotalBytes: 10_000,
		ResetPolicy: policy, ResetDay: resetDay, LastResetAt: lastResetAt,
	}).Error; err != nil {
		t.Fatalf("create node quota %q: %v", email, err)
	}
	if err := db.Create(&model.ClientNodeUsage{
		ClientId: record.Id, NodeId: nodeID, Up: up, Down: down, CycleStartedAt: 1,
	}).Error; err != nil {
		t.Fatalf("create node usage %q: %v", email, err)
	}
	return seededNodeQuota{clientID: record.Id, nodeID: nodeID}
}

func scheduledNodeUsage(t *testing.T, seed seededNodeQuota) model.ClientNodeUsage {
	t.Helper()
	var usage model.ClientNodeUsage
	if err := database.GetDB().Where("client_id = ? AND node_id = ?", seed.clientID, seed.nodeID).First(&usage).Error; err != nil {
		t.Fatal(err)
	}
	return usage
}

func TestPeriodicTrafficResetNodeQuotas(t *testing.T) {
	t.Run("daily resets only a cycle not reset today and never stays untouched", func(t *testing.T) {
		initResetJobDB(t)
		now := time.Now().UTC()
		due := seedScheduledNodeQuota(t, "node-daily-due@x", "daily", 1, now.AddDate(0, 0, -1).UnixMilli(), 50, 60)
		already := seedScheduledNodeQuota(t, "node-daily-current@x", "daily", 1, now.UnixMilli(), 70, 80)
		never := seedScheduledNodeQuota(t, "node-never@x", "never", 1, 0, 90, 100)

		NewPeriodicTrafficResetJob("daily", time.UTC).Run()

		if got := scheduledNodeUsage(t, due); got.Up != 0 || got.Down != 0 {
			t.Fatalf("due daily quota not reset: %+v", got)
		}
		if got := scheduledNodeUsage(t, already); got.Up != 70 || got.Down != 80 {
			t.Fatalf("already-reset daily quota reset twice: %+v", got)
		}
		if got := scheduledNodeUsage(t, never); got.Up != 90 || got.Down != 100 {
			t.Fatalf("never quota was reset: %+v", got)
		}
	})

	t.Run("monthly honors reset day and prevents a second reset in the month", func(t *testing.T) {
		initResetJobDB(t)
		now := time.Now().UTC()
		today := now.Day()
		otherDay := today%28 + 1
		due := seedScheduledNodeQuota(t, "node-monthly-due@x", "monthly", today, now.AddDate(0, -1, 0).UnixMilli(), 11, 12)
		notDue := seedScheduledNodeQuota(t, "node-monthly-wait@x", "monthly", otherDay, now.AddDate(0, -1, 0).UnixMilli(), 13, 14)

		job := NewPeriodicTrafficResetJob("monthly", time.UTC)
		job.Run()
		if got := scheduledNodeUsage(t, due); got.Up != 0 || got.Down != 0 {
			t.Fatalf("due monthly quota not reset: %+v", got)
		}
		if got := scheduledNodeUsage(t, notDue); got.Up != 13 || got.Down != 14 {
			t.Fatalf("monthly quota on another day reset: %+v", got)
		}

		if err := database.GetDB().Model(&model.ClientNodeUsage{}).
			Where("client_id = ? AND node_id = ?", due.clientID, due.nodeID).
			Updates(map[string]any{"up": 21, "down": 22}).Error; err != nil {
			t.Fatal(err)
		}
		job.Run()
		if got := scheduledNodeUsage(t, due); got.Up != 21 || got.Down != 22 {
			t.Fatalf("monthly quota reset twice in one month: %+v", got)
		}
	})

	t.Run("weekly last reset in this week is idempotent", func(t *testing.T) {
		initResetJobDB(t)
		now := time.Now().UTC()
		due := seedScheduledNodeQuota(t, "node-weekly-due@x", "weekly", 1, now.AddDate(0, 0, -8).UnixMilli(), 31, 32)
		current := seedScheduledNodeQuota(t, "node-weekly-current@x", "weekly", 1, now.UnixMilli(), 33, 34)

		NewPeriodicTrafficResetJob("weekly", time.UTC).Run()
		if got := scheduledNodeUsage(t, due); got.Up != 0 || got.Down != 0 {
			t.Fatalf("due weekly quota not reset: %+v", got)
		}
		if got := scheduledNodeUsage(t, current); got.Up != 33 || got.Down != 34 {
			t.Fatalf("current weekly quota reset twice: %+v", got)
		}
	})
}
