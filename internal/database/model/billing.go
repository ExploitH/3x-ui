package model

// NodeCostProfile is a versioned recurring cost profile for one physical node.
// Monetary values use integer minor units (for example CNY fen or USD cents)
// instead of float so billing reports remain reproducible.
type NodeCostProfile struct {
	Id                     int    `json:"id" gorm:"primaryKey;autoIncrement"`
	NodeId                 int    `json:"nodeId" gorm:"column:node_id;not null;index:idx_node_cost_profile,priority:1"`
	Provider               string `json:"provider" gorm:"not null;default:''"`
	AmountMinor            int64  `json:"amountMinor" gorm:"column:amount_minor;not null;default:0"`
	Currency               string `json:"currency" gorm:"not null;default:''"`
	BillingCycle           string `json:"billingCycle" gorm:"column:billing_cycle;not null;default:monthly;index"`
	IncludedTrafficBytes   int64  `json:"includedTrafficBytes" gorm:"column:included_traffic_bytes;not null;default:0"`
	TrafficCalcType        string `json:"trafficCalcType" gorm:"column:traffic_calc_type;not null;default:total"`
	OveragePriceMinorPerGB int64  `json:"overagePriceMinorPerGb" gorm:"column:overage_price_minor_per_gb;not null;default:0"`
	EffectiveFrom          int64  `json:"effectiveFrom" gorm:"column:effective_from;not null;index:idx_node_cost_profile,priority:2"`
	EffectiveTo            int64  `json:"effectiveTo" gorm:"column:effective_to;not null;default:0"`
	CreatedAt              int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
	UpdatedAt              int64  `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}

func (NodeCostProfile) TableName() string { return "node_cost_profiles" }

// InfrastructureCost records non-VPS recurring infrastructure expenses such as
// domains, CDT, Cloudflare/L4 proxy, tunnels, and other forwarding services.
type InfrastructureCost struct {
	Id             int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Type           string `json:"type" gorm:"not null;index"`
	Name           string `json:"name" gorm:"not null;default:''"`
	AmountMinor    int64  `json:"amountMinor" gorm:"column:amount_minor;not null;default:0"`
	Currency       string `json:"currency" gorm:"not null;default:''"`
	BillingCycle   string `json:"billingCycle" gorm:"column:billing_cycle;not null;default:monthly;index"`
	AllocationMode string `json:"allocationMode" gorm:"column:allocation_mode;not null;default:infrastructure"`
	NodeId         *int   `json:"nodeId" gorm:"column:node_id;index"`
	PathId         string `json:"pathId" gorm:"column:path_id;not null;default:'';index"`
	MetadataJSON   string `json:"metadataJson" gorm:"column:metadata_json;type:text"`
	EffectiveFrom  int64  `json:"effectiveFrom" gorm:"column:effective_from;not null;index"`
	EffectiveTo    int64  `json:"effectiveTo" gorm:"column:effective_to;not null;default:0"`
	CreatedAt      int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
	UpdatedAt      int64  `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}

func (InfrastructureCost) TableName() string { return "infrastructure_costs" }

// NodeTrafficCycle separates physical/NIC and provider-import accounting from
// per-client Xray/sing-box counters. A cycle may have multiple sources for
// reconciliation, but provider_billable_bytes is never silently copied into
// ingress and egress.
type NodeTrafficCycle struct {
	Id                    int    `json:"id" gorm:"primaryKey;autoIncrement"`
	NodeId                int    `json:"nodeId" gorm:"column:node_id;not null;uniqueIndex:idx_node_traffic_cycle_identity,priority:1;index:idx_node_traffic_cycle,priority:1"`
	CycleStart            int64  `json:"cycleStart" gorm:"column:cycle_start;not null;uniqueIndex:idx_node_traffic_cycle_identity,priority:2;index:idx_node_traffic_cycle,priority:2"`
	CycleEnd              int64  `json:"cycleEnd" gorm:"column:cycle_end;not null;default:0"`
	IngressBytes          int64  `json:"ingressBytes" gorm:"column:ingress_bytes;not null;default:0"`
	EgressBytes           int64  `json:"egressBytes" gorm:"column:egress_bytes;not null;default:0"`
	ProviderBillableBytes int64  `json:"providerBillableBytes" gorm:"column:provider_billable_bytes;not null;default:0"`
	Source                string `json:"source" gorm:"not null;index:idx_node_traffic_cycle,priority:3;uniqueIndex:idx_node_traffic_cycle_identity,priority:3"`
	TrafficCalcType       string `json:"trafficCalcType" gorm:"column:traffic_calc_type;not null;default:total"`
	CreatedAt             int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
	UpdatedAt             int64  `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}

func (NodeTrafficCycle) TableName() string { return "node_traffic_cycles" }

// BillingFxRate is optional and dated. Currencies remain separate when no
// matching observed rate exists; reports must not invent an exchange rate.
type BillingFxRate struct {
	Id            int    `json:"id" gorm:"primaryKey;autoIncrement"`
	BaseCurrency  string `json:"baseCurrency" gorm:"column:base_currency;not null;index:idx_billing_fx,priority:1"`
	QuoteCurrency string `json:"quoteCurrency" gorm:"column:quote_currency;not null;index:idx_billing_fx,priority:2"`
	RateMicros    int64  `json:"rateMicros" gorm:"column:rate_micros;not null;default:0"`
	ObservedAt    int64  `json:"observedAt" gorm:"column:observed_at;not null;index:idx_billing_fx,priority:3"`
	Source        string `json:"source" gorm:"not null;default:''"`
	CreatedAt     int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
}

func (BillingFxRate) TableName() string { return "billing_fx_rates" }
