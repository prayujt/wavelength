package realtime

import "testing"

func TestPrivateChangesAndSharedPublications(t *testing.T) {
	h := NewHub()
	alice, stopAlice := h.Subscribe("alice")
	bob, stopBob := h.Subscribe("bob")
	defer stopAlice()
	defer stopBob()
	h.Publish(Event{Owner: "alice", Change: ImportChanged{ID: "private"}})
	if (<-alice).Change.(ImportChanged).ID != "private" {
		t.Fatal("missing own job")
	}
	select {
	case <-bob:
		t.Fatal("another user's job was disclosed")
	default:
	}
	h.Publish(Event{Change: TrackPublished{TrackID: "shared"}})
	for _, events := range []<-chan Event{alice, bob} {
		if (<-events).Change.(TrackPublished).TrackID != "shared" {
			t.Fatal("missing shared publication")
		}
	}
}

func TestSlowReaderDisconnectsWithoutBlockingOtherReaders(t *testing.T) {
	h := NewHub()
	slow, stopSlow := h.Subscribe("alice")
	fast, stopFast := h.Subscribe("alice")
	defer stopFast()
	for range 100 {
		h.Publish(Event{Owner: "alice", Change: ImportChanged{ID: "job"}})
		<-fast
	}
	for range slow {
	}
	stopSlow()
	stopSlow()
	if len(h.subscribers) != 1 {
		t.Fatal("slow subscription was not removed")
	}
}
