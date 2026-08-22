package runtime

import (
	"testing"
	"time"
)

func TestMapSingboxTrafficUsersToCanonicalEmails(t *testing.T) {
	snapshot := &SingboxTrafficSnapshot{
		Source:     singboxTrafficSource,
		CapturedAt: testCapturedAt(),
		Users: []SingboxUserTraffic{
			{Name: " Alice@Example.com ", Uplink: 10, Downlink: 20, Total: 30},
			{Name: "bob@example.com", Uplink: 3, Downlink: 4, Total: 7},
		},
	}
	mapped, err := MapSingboxTrafficUsers(snapshot, []string{"alice@example.com", "bob@example.com"})
	if err != nil {
		t.Fatalf("MapSingboxTrafficUsers: %v", err)
	}
	if len(mapped) != 2 || mapped[0].Email != "alice@example.com" || mapped[1].Email != "bob@example.com" {
		t.Fatalf("mapped=%+v", mapped)
	}
	if mapped[0].Uplink != 10 || mapped[0].Downlink != 20 || mapped[0].Total != 30 {
		t.Fatalf("mapped alice=%+v", mapped[0])
	}
}

func TestMapSingboxTrafficUsersRejectsUnknownIdentity(t *testing.T) {
	snapshot := &SingboxTrafficSnapshot{
		Source:     singboxTrafficSource,
		CapturedAt: testCapturedAt(),
		Users:      []SingboxUserTraffic{{Name: "unknown@example.com", Uplink: 1, Downlink: 2, Total: 3}},
	}
	if _, err := MapSingboxTrafficUsers(snapshot, []string{"alice@example.com"}); err == nil {
		t.Fatal("unknown identity accepted")
	}
}

func TestMapSingboxTrafficUsersRejectsCanonicalCollision(t *testing.T) {
	snapshot := &SingboxTrafficSnapshot{
		Source:     singboxTrafficSource,
		CapturedAt: testCapturedAt(),
		Users: []SingboxUserTraffic{
			{Name: "Alice@example.com", Uplink: 1, Downlink: 1, Total: 2},
			{Name: "alice@example.com", Uplink: 2, Downlink: 2, Total: 4},
		},
	}
	if _, err := MapSingboxTrafficUsers(snapshot, []string{"alice@example.com"}); err == nil {
		t.Fatal("canonical identity collision accepted")
	}
}

func testCapturedAt() time.Time { return time.Unix(100, 0) }
