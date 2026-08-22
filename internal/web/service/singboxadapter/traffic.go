package singboxadapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	userUplinkPattern      = `^user>>>.*>>>traffic>>>uplink$`
	userDownlinkPattern    = `^user>>>.*>>>traffic>>>downlink$`
	inboundUplinkPattern   = `^inbound>>>.*>>>traffic>>>uplink$`
	inboundDownlinkPattern = `^inbound>>>.*>>>traffic>>>downlink$`
)

var (
	errTrafficStatsNotConfigured = errors.New("sing-box V2Ray traffic stats are not configured")
	errTrafficUserNameMissing    = errors.New("sing-box traffic user name is required")
)

type trafficPlan struct {
	users    []string
	inbounds []string
}

type trafficBytes struct {
	uplink      int64
	downlink    int64
	uplinkSet   bool
	downlinkSet bool
}

type ReadOnlyTrafficSnapshot struct {
	Source     string                   `json:"source"`
	CapturedAt time.Time                `json:"capturedAt"`
	Users      []ReadOnlyUserTraffic    `json:"users"`
	Inbounds   []ReadOnlyInboundTraffic `json:"inbounds"`
}

type ReadOnlyUserTraffic struct {
	Name     string `json:"name"`
	Uplink   int64  `json:"uplink"`
	Downlink int64  `json:"downlink"`
	Total    int64  `json:"total"`
}

type ReadOnlyInboundTraffic struct {
	Tag      string `json:"tag"`
	Uplink   int64  `json:"uplink"`
	Downlink int64  `json:"downlink"`
	Total    int64  `json:"total"`
}

func buildTrafficPlan(cfg singboxConfig) (trafficPlan, error) {
	api := cfg.Experimental.V2RayAPI
	if api == nil || !api.Stats.Enabled || strings.TrimSpace(api.Listen) == "" {
		return trafficPlan{}, errTrafficStatsNotConfigured
	}
	if err := validateV2RayAPIAddress(api.Listen); err != nil {
		return trafficPlan{}, fmt.Errorf("sing-box V2Ray API listen: %w", err)
	}
	if len(api.Stats.Users) == 0 || len(api.Stats.Inbounds) == 0 {
		return trafficPlan{}, errors.New("sing-box V2Ray stats users and inbounds are required")
	}

	configuredUsers := make(map[string]struct{}, len(api.Stats.Users))
	for _, name := range api.Stats.Users {
		name = strings.TrimSpace(name)
		if name == "" || strings.Contains(name, ">>>") {
			return trafficPlan{}, fmt.Errorf("invalid sing-box stats user name %q", name)
		}
		configuredUsers[name] = struct{}{}
	}
	configuredInbounds := make(map[string]struct{}, len(api.Stats.Inbounds))
	configuredInboundOrder := make([]string, 0, len(api.Stats.Inbounds))
	for _, tag := range api.Stats.Inbounds {
		tag = strings.TrimSpace(tag)
		if tag == "" || strings.Contains(tag, ">>>") {
			return trafficPlan{}, fmt.Errorf("invalid sing-box stats inbound tag %q", tag)
		}
		if _, exists := configuredInbounds[tag]; exists {
			continue
		}
		configuredInbounds[tag] = struct{}{}
		configuredInboundOrder = append(configuredInboundOrder, tag)
	}

	users := make(map[string]struct{})
	configInbounds := make(map[string]singboxInbound, len(cfg.Inbounds))
	for _, inbound := range cfg.Inbounds {
		tag := strings.TrimSpace(inbound.Tag)
		if tag != "" {
			configInbounds[tag] = inbound
		}
	}
	inbounds := make([]string, 0, len(configuredInboundOrder))
	for _, tag := range configuredInboundOrder {
		inbound, ok := configInbounds[tag]
		if !ok {
			return trafficPlan{}, fmt.Errorf("sing-box stats inbound %q is missing from config", tag)
		}
		inbounds = append(inbounds, tag)
		names, err := inboundUserNames(inbound)
		if err != nil {
			return trafficPlan{}, err
		}
		for _, name := range names {
			if _, ok := configuredUsers[name]; !ok {
				return trafficPlan{}, fmt.Errorf("sing-box stats users missing user name %q", name)
			}
			users[name] = struct{}{}
		}
	}
	if len(users) == 0 {
		return trafficPlan{}, errTrafficUserNameMissing
	}
	plan := trafficPlan{
		users:    make([]string, 0, len(users)),
		inbounds: inbounds,
	}
	for name := range users {
		plan.users = append(plan.users, name)
	}
	sort.Strings(plan.users)
	sort.Strings(plan.inbounds)
	return plan, nil
}

func inboundUserNames(inbound singboxInbound) ([]string, error) {
	names := make([]string, 0, len(inbound.Users))
	seen := make(map[string]struct{}, len(inbound.Users))
	for _, raw := range inbound.Users {
		var user struct {
			Name     string `json:"name"`
			Username string `json:"username"`
		}
		if err := json.Unmarshal(raw, &user); err != nil {
			return nil, fmt.Errorf("decode users for inbound %q: %w", inbound.Tag, err)
		}
		name := strings.TrimSpace(user.Name)
		if name == "" {
			name = strings.TrimSpace(user.Username)
		}
		if name == "" {
			return nil, fmt.Errorf("%w for inbound %q", errTrafficUserNameMissing, inbound.Tag)
		}
		if strings.Contains(name, ">>>") {
			return nil, fmt.Errorf("invalid sing-box user name %q for inbound %q", name, inbound.Tag)
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names, nil
}

func (h *ReadOnlyHandler) handleTrafficSnapshot(w http.ResponseWriter, r *http.Request) {
	if h.stats == nil {
		writeReadOnlyJSON(w, http.StatusServiceUnavailable, readOnlyEnvelope{Success: false, Msg: errTrafficStatsNotConfigured.Error()})
		return
	}
	cfg, err := h.loadConfig()
	if err != nil {
		writeReadOnlyJSON(w, http.StatusServiceUnavailable, readOnlyEnvelope{Success: false, Msg: err.Error()})
		return
	}
	plan, err := buildTrafficPlan(cfg)
	if err != nil {
		writeReadOnlyJSON(w, http.StatusServiceUnavailable, readOnlyEnvelope{Success: false, Msg: err.Error()})
		return
	}
	stats, err := h.stats.Query(r.Context(), []string{
		userUplinkPattern,
		userDownlinkPattern,
		inboundUplinkPattern,
		inboundDownlinkPattern,
	})
	if err != nil {
		writeReadOnlyJSON(w, http.StatusServiceUnavailable, readOnlyEnvelope{Success: false, Msg: err.Error()})
		return
	}
	snapshot, err := makeTrafficSnapshot(plan, stats)
	if err != nil {
		writeReadOnlyJSON(w, http.StatusServiceUnavailable, readOnlyEnvelope{Success: false, Msg: err.Error()})
		return
	}
	writeReadOnlyJSON(w, http.StatusOK, readOnlyEnvelope{Success: true, Obj: snapshot})
}

func makeTrafficSnapshot(plan trafficPlan, stats []TrafficStat) (*ReadOnlyTrafficSnapshot, error) {
	users := make(map[string]*trafficBytes, len(plan.users))
	for _, name := range plan.users {
		users[name] = &trafficBytes{}
	}
	inbounds := make(map[string]*trafficBytes, len(plan.inbounds))
	for _, tag := range plan.inbounds {
		inbounds[tag] = &trafficBytes{}
	}
	for _, stat := range stats {
		kind, key, direction, ok := parseTrafficCounterName(stat.Name)
		if !ok {
			continue
		}
		if stat.Value < 0 {
			return nil, fmt.Errorf("negative sing-box traffic counter %q", stat.Name)
		}
		var counters *trafficBytes
		switch kind {
		case "user":
			counters = users[key]
		case "inbound":
			counters = inbounds[key]
		}
		if counters == nil {
			continue
		}
		switch direction {
		case "uplink":
			if counters.uplinkSet {
				return nil, fmt.Errorf("duplicate sing-box traffic counter %q", stat.Name)
			}
			counters.uplink, counters.uplinkSet = stat.Value, true
		case "downlink":
			if counters.downlinkSet {
				return nil, fmt.Errorf("duplicate sing-box traffic counter %q", stat.Name)
			}
			counters.downlink, counters.downlinkSet = stat.Value, true
		}
	}

	snapshot := &ReadOnlyTrafficSnapshot{
		Source:     "sing-box-v2ray-api",
		CapturedAt: time.Now().UTC(),
		Users:      make([]ReadOnlyUserTraffic, 0, len(plan.users)),
		Inbounds:   make([]ReadOnlyInboundTraffic, 0, len(plan.inbounds)),
	}
	for _, name := range plan.users {
		counter := users[name]
		if !counter.uplinkSet || !counter.downlinkSet {
			return nil, fmt.Errorf("traffic counter unavailable for user %q", name)
		}
		total, err := trafficTotal(counter.uplink, counter.downlink)
		if err != nil {
			return nil, fmt.Errorf("user %q: %w", name, err)
		}
		snapshot.Users = append(snapshot.Users, ReadOnlyUserTraffic{Name: name, Uplink: counter.uplink, Downlink: counter.downlink, Total: total})
	}
	for _, tag := range plan.inbounds {
		counter := inbounds[tag]
		if !counter.uplinkSet || !counter.downlinkSet {
			return nil, fmt.Errorf("traffic counter unavailable for inbound %q", tag)
		}
		total, err := trafficTotal(counter.uplink, counter.downlink)
		if err != nil {
			return nil, fmt.Errorf("inbound %q: %w", tag, err)
		}
		snapshot.Inbounds = append(snapshot.Inbounds, ReadOnlyInboundTraffic{Tag: tag, Uplink: counter.uplink, Downlink: counter.downlink, Total: total})
	}
	return snapshot, nil
}

func parseTrafficCounterName(name string) (kind, key, direction string, ok bool) {
	parts := strings.Split(name, ">>>")
	if len(parts) != 4 || parts[2] != "traffic" || (parts[3] != "uplink" && parts[3] != "downlink") {
		return "", "", "", false
	}
	if parts[0] != "user" && parts[0] != "inbound" {
		return "", "", "", false
	}
	if parts[1] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[3], true
}

func trafficTotal(uplink, downlink int64) (int64, error) {
	const maxInt64 = int64(^uint64(0) >> 1)
	if uplink < 0 || downlink < 0 || downlink > maxInt64-uplink {
		return 0, errors.New("traffic counter overflow")
	}
	return uplink + downlink, nil
}
