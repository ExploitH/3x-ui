package runtime

import (
	"errors"
	"fmt"
	"time"
)

type SingboxTrafficDelta struct {
	Source     string                       `json:"source"`
	CapturedAt time.Time                    `json:"capturedAt"`
	Users      []SingboxUserTrafficDelta    `json:"users"`
	Inbounds   []SingboxInboundTrafficDelta `json:"inbounds"`
}

type SingboxUserTrafficDelta struct {
	Name      string `json:"name"`
	Uplink    int64  `json:"uplink"`
	Downlink  int64  `json:"downlink"`
	Total     int64  `json:"total"`
	FirstSeen bool   `json:"firstSeen"`
	Reset     bool   `json:"reset"`
}

type SingboxInboundTrafficDelta struct {
	Tag       string `json:"tag"`
	Uplink    int64  `json:"uplink"`
	Downlink  int64  `json:"downlink"`
	Total     int64  `json:"total"`
	FirstSeen bool   `json:"firstSeen"`
	Reset     bool   `json:"reset"`
}

func ComputeSingboxTrafficDelta(previous, current *SingboxTrafficSnapshot) (*SingboxTrafficDelta, error) {
	if current == nil {
		return nil, errors.New("current sing-box traffic snapshot is nil")
	}
	if err := validateSingboxTrafficSnapshot(current); err != nil {
		return nil, err
	}
	if previous != nil {
		if err := validateSingboxTrafficSnapshot(previous); err != nil {
			return nil, fmt.Errorf("previous sing-box traffic snapshot: %w", err)
		}
		if previous.Source != current.Source {
			return nil, fmt.Errorf("sing-box traffic snapshot source changed from %q to %q", previous.Source, current.Source)
		}
		if !current.CapturedAt.After(previous.CapturedAt) {
			return nil, fmt.Errorf("sing-box traffic snapshot timestamp moved backwards")
		}
	}

	delta := &SingboxTrafficDelta{
		Source:     current.Source,
		CapturedAt: current.CapturedAt,
		Users:      make([]SingboxUserTrafficDelta, 0, len(current.Users)),
		Inbounds:   make([]SingboxInboundTrafficDelta, 0, len(current.Inbounds)),
	}
	var previousUsers map[string]SingboxUserTraffic
	var previousInbounds map[string]SingboxInboundTraffic
	if previous != nil {
		previousUsers = make(map[string]SingboxUserTraffic, len(previous.Users))
		for _, row := range previous.Users {
			previousUsers[row.Name] = row
		}
		previousInbounds = make(map[string]SingboxInboundTraffic, len(previous.Inbounds))
		for _, row := range previous.Inbounds {
			previousInbounds[row.Tag] = row
		}
	}
	for _, row := range current.Users {
		previousRow, exists := previousUsers[row.Name]
		delta.Users = append(delta.Users, makeUserDelta(row, previousRow, exists))
	}
	for _, row := range current.Inbounds {
		previousRow, exists := previousInbounds[row.Tag]
		delta.Inbounds = append(delta.Inbounds, makeInboundDelta(row, previousRow, exists))
	}
	return delta, nil
}

func makeUserDelta(current, previous SingboxUserTraffic, exists bool) SingboxUserTrafficDelta {
	if !exists {
		return SingboxUserTrafficDelta{Name: current.Name, FirstSeen: true}
	}
	if current.Uplink < previous.Uplink || current.Downlink < previous.Downlink {
		return SingboxUserTrafficDelta{Name: current.Name, Uplink: current.Uplink, Downlink: current.Downlink, Total: current.Total, Reset: true}
	}
	return SingboxUserTrafficDelta{
		Name:     current.Name,
		Uplink:   current.Uplink - previous.Uplink,
		Downlink: current.Downlink - previous.Downlink,
		Total:    current.Total - previous.Total,
	}
}

func makeInboundDelta(current, previous SingboxInboundTraffic, exists bool) SingboxInboundTrafficDelta {
	if !exists {
		return SingboxInboundTrafficDelta{Tag: current.Tag, FirstSeen: true}
	}
	if current.Uplink < previous.Uplink || current.Downlink < previous.Downlink {
		return SingboxInboundTrafficDelta{Tag: current.Tag, Uplink: current.Uplink, Downlink: current.Downlink, Total: current.Total, Reset: true}
	}
	return SingboxInboundTrafficDelta{
		Tag:      current.Tag,
		Uplink:   current.Uplink - previous.Uplink,
		Downlink: current.Downlink - previous.Downlink,
		Total:    current.Total - previous.Total,
	}
}
