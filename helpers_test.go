package rushline

import (
	"testing"
	"time"
)

type holdResult struct {
	hold Hold
	err  error
}

var now = time.Unix(1781622600, 0)

func setupEvent(tb testing.TB, now time.Time, capacity int) *Event {
	tb.Helper()
	event, err := newEvent("1", capacity, now, now.Add(time.Hour), now.Add(time.Hour+time.Minute))
	if err != nil {
		tb.Fatalf("tried creating a valid event, but got: '%v'", err.Error())
	}
	return event
}

func setupHold(t *testing.T, event *Event, now time.Time) Hold {
	t.Helper()
	hold, err := event.createHold("1", 1, now, time.Minute)
	if err != nil {
		t.Fatalf("tried creating a valid hold, but got: '%v'", err.Error())
	}
	return hold
}

func countOccurrences[T comparable](slice []T, target T) int {
	count := 0
	for _, v := range slice {
		if v == target {
			count++
		}
	}
	return count
}

func assertExpirableHoldMembership(t *testing.T, e *Event) {
	t.Helper()
	for id := range e.holds {
		occurrences := countOccurrences(e.expirableHolds, id)
		if e.holds[id].status == HoldStatusActive {
			if occurrences == 0 {
				t.Errorf("expected hold with id '%v' to be in expirableHolds exactly once, but not found", id)
			}
			if occurrences > 1 {
				t.Errorf("expected hold with id '%v' to be in expirableHolds exactly once, but contained %v", id, occurrences)
			}
		} else {
			if occurrences != 0 {
				t.Errorf("expected hold with id '%v' to not be found in expirableHolds, but contained %v", id, occurrences)
			}
		}
	}
	for _, id := range e.expirableHolds {
		_, exists := e.getHoldByID(id)
		if !exists {
			t.Errorf("expected expirableHold entry with id '%v' to be in holds map", id)
		}
	}
}
