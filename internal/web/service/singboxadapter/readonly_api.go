package singboxadapter

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"net/http"
	"os"
	"strings"
	"time"
)

const readonlyPanelVersion = "singbox-adapter-readonly"

type ReadOnlyOptions struct {
	ConfigPath      string
	ConfigJSON      []byte
	Token           string
	BasePath        string
	V2RayAPIAddress string
	StatsProvider   TrafficStatsProvider
}

type ReadOnlyHandler struct {
	token       []byte
	basePath    string
	configPath  string
	configJSON  []byte
	startedAt   time.Time
	stats       TrafficStatsProvider
	statsCloser func() error
}

type singboxConfig struct {
	Inbounds     []singboxInbound    `json:"inbounds"`
	Experimental singboxExperimental `json:"experimental"`
}

type singboxExperimental struct {
	V2RayAPI *singboxV2RayAPI `json:"v2ray_api"`
}

type singboxV2RayAPI struct {
	Listen string          `json:"listen"`
	Stats  singboxStatsAPI `json:"stats"`
}

type singboxStatsAPI struct {
	Enabled  bool     `json:"enabled"`
	Inbounds []string `json:"inbounds"`
	Users    []string `json:"users"`
}

type singboxInbound struct {
	Type       string            `json:"type"`
	Tag        string            `json:"tag"`
	Listen     string            `json:"listen"`
	ListenPort int               `json:"listen_port"`
	Users      []json.RawMessage `json:"users"`
}

type ReadOnlyInbound struct {
	Id       int    `json:"id"`
	Tag      string `json:"tag"`
	Remark   string `json:"remark"`
	Listen   string `json:"listen"`
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
	Users    int    `json:"users"`
}

type ReadOnlyCapabilities struct {
	Mode             string `json:"mode"`
	TrafficSource    string `json:"trafficSource,omitempty"`
	Config           bool   `json:"config"`
	InboundInventory bool   `json:"inboundInventory"`
	ClientCrud       bool   `json:"clientCrud"`
	ClientEnable     bool   `json:"clientEnable"`
	PerClientTraffic bool   `json:"perClientTraffic"`
	TrafficReset     bool   `json:"trafficReset"`
	ClientIP         bool   `json:"clientIp"`
	RelayIdentity    bool   `json:"relayIdentity"`
}

var readonlyCapabilities = ReadOnlyCapabilities{
	Mode:             "readonly",
	Config:           true,
	InboundInventory: true,
	ClientCrud:       false,
	ClientEnable:     false,
	PerClientTraffic: false,
	TrafficReset:     false,
	ClientIP:         false,
	RelayIdentity:    false,
}

type readOnlyEnvelope struct {
	Success bool   `json:"success"`
	Msg     string `json:"msg,omitempty"`
	Obj     any    `json:"obj,omitempty"`
}

func NewReadOnlyHandler(options ReadOnlyOptions) (*ReadOnlyHandler, error) {
	normalizedToken := strings.TrimSpace(options.Token)
	if normalizedToken == "" {
		return nil, errors.New("sing-box adapter token is required")
	}
	basePath := normalizeAdapterBasePath(options.BasePath)
	if options.ConfigPath == "" && len(options.ConfigJSON) == 0 {
		return nil, errors.New("sing-box adapter config path or config JSON is required")
	}
	h := &ReadOnlyHandler{
		token:      []byte(normalizedToken),
		basePath:   basePath,
		configPath: options.ConfigPath,
		configJSON: append([]byte(nil), options.ConfigJSON...),
		startedAt:  time.Now(),
		stats:      options.StatsProvider,
	}
	if h.stats == nil && strings.TrimSpace(options.V2RayAPIAddress) != "" {
		client, err := dialV2RayStatsClient(context.Background(), strings.TrimSpace(options.V2RayAPIAddress))
		if err != nil {
			return nil, err
		}
		h.stats = client
		h.statsCloser = client.Close
	}
	if _, err := h.loadConfig(); err != nil {
		_ = h.Close()
		return nil, err
	}
	return h, nil
}

func (h *ReadOnlyHandler) Close() error {
	if h == nil || h.statsCloser == nil {
		return nil
	}
	return h.statsCloser()
}

func normalizeAdapterBasePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/adapter/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path
}

func (h *ReadOnlyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, h.basePath) {
		http.NotFound(w, r)
		return
	}
	if !h.authorized(r.Header.Get("Authorization")) {
		writeReadOnlyJSON(w, http.StatusUnauthorized, readOnlyEnvelope{Success: false, Msg: "unauthorized"})
		return
	}
	if r.Method != http.MethodGet {
		writeReadOnlyJSON(w, http.StatusMethodNotAllowed, readOnlyEnvelope{Success: false, Msg: "read-only adapter"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, h.basePath)
	switch path {
	case "panel/api/inbounds/list":
		h.handleInboundList(w)
	case "panel/api/server/status":
		h.handleStatus(w)
	case "panel/api/server/capabilities":
		h.handleCapabilities(w)
	case "panel/api/traffic/snapshot":
		h.handleTrafficSnapshot(w, r)
	case "healthz":
		writeReadOnlyJSON(w, http.StatusOK, readOnlyEnvelope{Success: true, Obj: map[string]any{"mode": "readonly"}})
	default:
		http.NotFound(w, r)
	}
}

func (h *ReadOnlyHandler) authorized(value string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	provided := []byte(strings.TrimSpace(strings.TrimPrefix(value, prefix)))
	return len(provided) == len(h.token) && subtle.ConstantTimeCompare(provided, h.token) == 1
}

func (h *ReadOnlyHandler) loadConfig() (singboxConfig, error) {
	data := h.configJSON
	if h.configPath != "" {
		var err error
		data, err = os.ReadFile(h.configPath)
		if err != nil {
			return singboxConfig{}, fmt.Errorf("read sing-box config: %w", err)
		}
	}
	var cfg singboxConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return singboxConfig{}, fmt.Errorf("decode sing-box config: %w", err)
	}
	seen := make(map[string]struct{}, len(cfg.Inbounds))
	for _, inbound := range cfg.Inbounds {
		if strings.TrimSpace(inbound.Tag) == "" {
			return singboxConfig{}, errors.New("sing-box inbound tag is required")
		}
		if _, exists := seen[inbound.Tag]; exists {
			return singboxConfig{}, fmt.Errorf("duplicate sing-box inbound tag %q", inbound.Tag)
		}
		if inbound.ListenPort < 1 || inbound.ListenPort > 65535 {
			return singboxConfig{}, fmt.Errorf("sing-box inbound %q has invalid port", inbound.Tag)
		}
		seen[inbound.Tag] = struct{}{}
	}
	return cfg, nil
}

func (h *ReadOnlyHandler) handleInboundList(w http.ResponseWriter) {
	cfg, err := h.loadConfig()
	if err != nil {
		writeReadOnlyJSON(w, http.StatusServiceUnavailable, readOnlyEnvelope{Success: false, Msg: err.Error()})
		return
	}
	items := make([]ReadOnlyInbound, 0, len(cfg.Inbounds))
	ids := make(map[int]string, len(cfg.Inbounds))
	for _, inbound := range cfg.Inbounds {
		id := stableInboundID(inbound.Tag)
		if prior, exists := ids[id]; exists && prior != inbound.Tag {
			writeReadOnlyJSON(w, http.StatusServiceUnavailable, readOnlyEnvelope{Success: false, Msg: "inbound id collision"})
			return
		}
		ids[id] = inbound.Tag
		items = append(items, ReadOnlyInbound{
			Id: id, Tag: inbound.Tag, Remark: inbound.Tag, Listen: inbound.Listen,
			Protocol: inbound.Type, Port: inbound.ListenPort, Users: len(inbound.Users),
		})
	}
	writeReadOnlyJSON(w, http.StatusOK, readOnlyEnvelope{Success: true, Obj: items})
}

func (h *ReadOnlyHandler) handleStatus(w http.ResponseWriter) {
	writeReadOnlyJSON(w, http.StatusOK, readOnlyEnvelope{Success: true, Obj: map[string]any{
		"cpu": 0,
		"mem": map[string]any{"current": 0, "total": 0},
		"xray": map[string]any{
			"version":  "sing-box-readonly",
			"state":    "not-applicable",
			"errorMsg": "sing-box adapter does not expose an Xray API",
		},
		"panelVersion": readonlyPanelVersion,
		"panelGuid":    h.panelGuid(),
		"uptime":       uint64(time.Since(h.startedAt).Seconds()),
		"netIO":        map[string]any{"up": 0, "down": 0},
	}})
}

func (h *ReadOnlyHandler) handleCapabilities(w http.ResponseWriter) {
	cfg, err := h.loadConfig()
	if err != nil {
		writeReadOnlyJSON(w, http.StatusServiceUnavailable, readOnlyEnvelope{Success: false, Msg: err.Error()})
		return
	}
	caps := readonlyCapabilities
	if h.stats != nil {
		if _, err := buildTrafficPlan(cfg); err == nil {
			caps.Mode = "traffic-readonly"
			caps.TrafficSource = "sing-box-v2ray-api"
			caps.PerClientTraffic = true
		}
	}
	writeReadOnlyJSON(w, http.StatusOK, readOnlyEnvelope{Success: true, Obj: caps})
}

func (h *ReadOnlyHandler) panelGuid() string {
	sum := sha256.Sum256(append([]byte(h.basePath), h.token...))
	return "singbox-" + hex.EncodeToString(sum[:8])
}

func stableInboundID(tag string) int {
	id := int(crc32.ChecksumIEEE([]byte(tag)) & 0x7fffffff)
	if id == 0 {
		return 1
	}
	return id
}

func writeReadOnlyJSON(w http.ResponseWriter, status int, value readOnlyEnvelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
