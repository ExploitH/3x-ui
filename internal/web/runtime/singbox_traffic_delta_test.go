package runtime

import (
	"testing"
	"time"
)

func TestComputeSingboxTrafficDeltaFirstSeenAndGrowth(t *testing.T) {
	first := singboxSnapshotForDelta(100, 200, 300, 400, time.Unix(100, 0))
	firstDelta, err := ComputeSingboxTrafficDelta(nil, first)
	if err != nil {
		t.Fatalf("first delta: %v", err)
	}
	if len(firstDelta.Users) != 1 || !firstDelta.Users[0].FirstSeen || firstDelta.Users[0].Uplink != 0 || firstDelta.Users[0].Downlink != 0 {
		t.Fatalf("first user delta=%+v", firstDelta.Users)
	}

	second := singboxSnapshotForDelta(130, 250, 350, 500, time.Unix(110, 0))
	growth, err := ComputeSingboxTrafficDelta(first, second)
	if err != nil {
		t.Fatalf("growth delta: %v", err)
	}
	user := growth.Users[0]
	inbound := growth.Inbounds[0]
	if user.FirstSeen || user.Reset || user.Uplink != 30 || user.Downlink != 50 {
		t.Fatalf("growth user delta=%+v", user)
	}
	if inbound.FirstSeen || inbound.Reset || inbound.Uplink != 50 || inbound.Downlink != 100 {
		t.Fatalf("growth inbound delta=%+v", inbound)
	}
}

func TestComputeSingboxTrafficDeltaTreatsCounterDecreaseAsReset(t *testing.T) {
	previous := singboxSnapshotForDelta(1000, 2000, 3000, 4000, time.Unix(100, 0))
	current := singboxSnapshotForDelta(10, 20, 30, 40, time.Unix(110, 0))
	delta, err := ComputeSingboxTrafficDelta(previous, current)
	if err != nil {
		t.Fatalf("reset delta: %v", err)
	}
	if !delta.Users[0].Reset || delta.Users[0].Uplink != 10 || delta.Users[0].Downlink != 20 {
		t.Fatalf("reset user delta=%+v", delta.Users[0])
	}
	if !delta.Inbounds[0].Reset || delta.Inbounds[0].Uplink != 30 || delta.Inbounds[0].Downlink != 40 {
		t.Fatalf("reset inbound delta=%+v", delta.Inbounds[0])
	}
}

func TestComputeSingboxTrafficDeltaRejectsStaleSnapshot(t *testing.T) {
	previous := singboxSnapshotForDelta(100, 200, 300, 400, time.Unix(110, 0))
	current := singboxSnapshotForDelta(130, 250, 350, 500, time.Unix(100, 0))
	if _, err := ComputeSingboxTrafficDelta(previous, current); err == nil {
		t.Fatal("stale snapshot accepted")
	}
}

func TestComputeSingboxTrafficDeltaRejectsSourceChange(t *testing.T) {
	previous := singboxSnapshotForDelta(100, 200, 300, 400, time.Unix(100, 0))
	current := singboxSnapshotForDelta(130, 250, 350, 500, time.Unix(110, 0))
	current.Source = "other-source"
	if _, err := ComputeSingboxTrafficDelta(previous, current); err == nil {
		t.Fatal("source change accepted")
	}
}

func TestComputeSingboxTrafficDeltaRejectsEqualTimestamp(t *testing.T) {
	previous := singboxSnapshotForDelta(100, 200, 300, 400, time.Unix(100, 0))
	current := singboxSnapshotForDelta(130, 250, 350, 500, time.Unix(100, 0))
	if _, err := ComputeSingboxTrafficDelta(previous, current); err == nil {
		t.Fatal("equal timestamp accepted")
	}
}

func singboxSnapshotForDelta(userUp, userDown, inboundUp, inboundDown int64, capturedAt time.Time) *SingboxTrafficSnapshot {
	return &SingboxTrafficSnapshot{
		Source:     singboxTrafficSource,
		CapturedAt: capturedAt,
		Users:      []SingboxUserTraffic{{Name: "alice", Uplink: userUp, Downlink: userDown, Total: userUp + userDown}},
		Inbounds:   []SingboxInboundTraffic{{Tag: "hk3-reality", Uplink: inboundUp, Downlink: inboundDown, Total: inboundUp + inboundDown}},
	}
}
