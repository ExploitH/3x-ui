package service

import (
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestClientNodeRestoreAllowedCanonicalWithoutLocalTraffic(t *testing.T) {
	setupConflictDB(t)
	db := database.GetDB()
	record := &model.ClientRecord{Email: "canonical@restore", Enable: true, TotalGB: 1 << 30}
	if err := db.Create(record).Error; err != nil {
		t.Fatal(err)
	}
	allowed, err := clientNodeRestoreAllowed(db, record)
	if err != nil || !allowed {
		t.Fatalf("no local/global traffic allowed=%v err=%v", allowed, err)
	}
	if err := db.Create(&model.ClientGlobalTraffic{MasterGuid: "master", Email: record.Email, Up: 1 << 30, Down: 0}).Error; err != nil {
		t.Fatal(err)
	}
	allowed, err = clientNodeRestoreAllowed(db, record)
	if err != nil || allowed {
		t.Fatalf("global exhausted allowed=%v err=%v", allowed, err)
	}
	if err := db.Model(record).Updates(map[string]any{"enable": false, "updated_at": time.Now().UnixMilli()}).Error; err != nil {
		t.Fatal(err)
	}
	allowed, err = clientNodeRestoreAllowed(db, record)
	if err != nil || allowed {
		t.Fatalf("admin disabled allowed=%v err=%v", allowed, err)
	}
}
