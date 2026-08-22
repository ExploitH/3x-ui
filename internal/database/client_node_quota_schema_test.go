package database

import (
	"path/filepath"
	"testing"
)

func TestPerNodeQuotaAutoMigrateCreatesSeparatedSchema(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	m := GetDB().Migrator()
	cases := []struct {
		table   string
		columns []string
		index   string
	}{
		{
			table: "client_node_quotas",
			columns: []string{
				"id", "client_id", "node_id", "total_bytes", "reset_policy",
				"reset_day", "last_reset_at", "created_at", "updated_at",
			},
			index: "idx_client_node_quota",
		},
		{
			table: "client_node_usages",
			columns: []string{
				"id", "client_id", "node_id", "up", "down", "cycle_started_at", "updated_at",
			},
			index: "idx_client_node_usage",
		},
		{
			table: "client_node_access_states",
			columns: []string{
				"id", "client_id", "node_id", "blocked", "reason", "blocked_at",
				"applied_at", "last_error", "updated_at",
			},
			index: "idx_client_node_access",
		},
	}

	for _, tc := range cases {
		t.Run(tc.table, func(t *testing.T) {
			if !m.HasTable(tc.table) {
				t.Fatalf("table %q was not created", tc.table)
			}
			for _, column := range tc.columns {
				if !m.HasColumn(tc.table, column) {
					t.Errorf("table %q missing column %q", tc.table, column)
				}
			}
			if !m.HasIndex(tc.table, tc.index) {
				t.Errorf("table %q missing unique index %q", tc.table, tc.index)
			}
		})
	}
}
