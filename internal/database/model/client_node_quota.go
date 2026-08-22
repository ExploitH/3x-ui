package model

const (
	ClientNodeAccessReasonQuotaExhausted = "node_quota_exhausted"
)

// ClientNodeQuota stores operator-owned node quota configuration. It is kept
// separate from NodeClientTraffic, whose sole job is raw remote-counter
// baselining, and from ClientNodeUsage, which is mutable accounting state.
type ClientNodeQuota struct {
	Id          int    `json:"id" gorm:"primaryKey;autoIncrement"`
	ClientId    int    `json:"clientId" gorm:"column:client_id;not null;uniqueIndex:idx_client_node_quota,priority:1;index"`
	NodeId      int    `json:"nodeId" gorm:"column:node_id;not null;uniqueIndex:idx_client_node_quota,priority:2;index"`
	TotalBytes  int64  `json:"totalBytes" gorm:"column:total_bytes;not null;default:0"`
	ResetPolicy string `json:"resetPolicy" gorm:"column:reset_policy;not null;default:never;index:idx_node_quota_reset,priority:1"`
	ResetDay    int    `json:"resetDay" gorm:"column:reset_day;not null;default:1"`
	LastResetAt int64  `json:"lastResetAt" gorm:"column:last_reset_at;not null;default:0;index:idx_node_quota_reset,priority:2"`
	CreatedAt   int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
	UpdatedAt   int64  `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}

func (ClientNodeQuota) TableName() string { return "client_node_quotas" }

// ClientNodeUsage is the accumulated per-client usage for the current quota
// cycle on one physical node. It advances from the same transaction that moves
// the corresponding NodeClientTraffic baseline.
type ClientNodeUsage struct {
	Id             int   `json:"id" gorm:"primaryKey;autoIncrement"`
	ClientId       int   `json:"clientId" gorm:"column:client_id;not null;uniqueIndex:idx_client_node_usage,priority:1;index"`
	NodeId         int   `json:"nodeId" gorm:"column:node_id;not null;uniqueIndex:idx_client_node_usage,priority:2;index"`
	Up             int64 `json:"up" gorm:"not null;default:0"`
	Down           int64 `json:"down" gorm:"not null;default:0"`
	CycleStartedAt int64 `json:"cycleStartedAt" gorm:"column:cycle_started_at;not null;default:0"`
	UpdatedAt      int64 `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}

// ClientNodeAccessState records why and whether a node-local block was applied.
// Canonical ClientRecord.Enable and ClientTraffic.Enable remain the global/admin
// desired state and are never repurposed for node-only quota exhaustion.
type ClientNodeAccessState struct {
	Id        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	ClientId  int    `json:"clientId" gorm:"column:client_id;not null;uniqueIndex:idx_client_node_access,priority:1;index"`
	NodeId    int    `json:"nodeId" gorm:"column:node_id;not null;uniqueIndex:idx_client_node_access,priority:2;index"`
	Blocked   bool   `json:"blocked" gorm:"not null;default:false;index"`
	Reason    string `json:"reason" gorm:"not null;default:'';index"`
	BlockedAt int64  `json:"blockedAt" gorm:"column:blocked_at;not null;default:0"`
	AppliedAt int64  `json:"appliedAt" gorm:"column:applied_at;not null;default:0"`
	LastError string `json:"lastError" gorm:"column:last_error"`
	UpdatedAt int64  `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}
