package rushline

import (
	"math"
	"testing"
	"time"
)

func setupEvent(t *testing.T, now time.Time) *Event {
	t.Helper()
	event, err := newEvent("1", 5, now, now.Add(time.Hour), now.Add(time.Hour+time.Minute))
	if err != nil {
		t.Fatalf("tried creating a valid event, but got: '%v'", err.Error())
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

func TestNewEvent(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name               string
		id                 string
		capacity           int
		onsaleOpening      time.Time
		holdCreationCutoff time.Time
		confirmationCutoff time.Time
		wantErr            string
	}{
		{"has id", "", 2, now, now.Add(time.Hour), now.Add(time.Hour + time.Minute), "id is required"},
		{"has non-zero capacity", "1", 0, now, now.Add(time.Hour), now.Add(time.Hour + time.Minute), "must request positive capacity"},
		{"has positive capacity", "1", -2, now, now.Add(time.Hour), now.Add(time.Hour + time.Minute), "must request positive capacity"},
		{"has hold creation cutoff occurring after onsale opening", "1", 2, now.Add(time.Hour), now, now.Add(time.Minute), "hold creation cutoff must be after onsale opening"},
		{"has confirmation cutoff occurring after hold creation cutoff", "1", 2, now, now.Add(time.Minute), now, "confirmation must be after hold creation cutoff"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := newEvent(tt.id, tt.capacity, tt.onsaleOpening, tt.holdCreationCutoff, tt.confirmationCutoff)
			if got == nil {
				t.Errorf("newEvent wanted error: '%v'", tt.wantErr)
			}
			if got != nil && got.Error() != tt.wantErr {
				t.Errorf("newEvent validation error: '%v'; want '%v'", got.Error(), tt.wantErr)
			}
		})
	}
}

func TestCreateHold(t *testing.T) {
	now := time.Now()

	t.Run("creates valid hold", func(t *testing.T) {
		event := setupEvent(t, now)
		hold := setupHold(t, event, now)
		availability := event.getAvailability()
		if hold.id != "1" {
			t.Errorf("expected id == '1', but got: %v", hold.id)
		}
		if hold.status != HoldStatusActive {
			t.Errorf("expected status == '1', but got: %v", hold.status)
		}
		if hold.quantity != 1 {
			t.Errorf("expected quantity == 1, but got: %v", hold.quantity)
		}
		if hold.deadline != now.Add(time.Minute) {
			t.Errorf("expected deadline == '%v' but got: %v", now.Add(time.Minute), hold.deadline)
		}
		if availability != 4 {
			t.Errorf("expected availability == 4, but got: %v", availability)
		}
	})

	tests := []struct {
		name              string
		needsExistingHold bool
		id                string
		quantity          int
		currentTime       time.Time
		holdDuration      time.Duration
		wantErr           string
	}{
		{"has id", false, "", 1, now.Add(time.Second), time.Hour, "id is required"},
		{"has unique id", true, "1", 1, now.Add(time.Second), time.Hour, "id is duplicate"},
		{"has future hold duration", false, "2", 1, now.Add(time.Second), -time.Hour, "holdDuration must be in the future"},
		{"has non-zero quantity", false, "2", 0, now.Add(time.Second), time.Hour, "must request positive quantity"},
		{"has positive quantity", false, "2", -1, now.Add(time.Second), time.Hour, "must request positive quantity"},
		{"has quantity not overflown", true, "2", math.MaxInt, now.Add(time.Second), time.Hour, "cannot over-sell"},
		{"has quantity within capacity", false, "2", 20, now.Add(time.Second), time.Hour, "cannot over-sell"},
		{"has early enough hold", false, "2", 1, now.Add(2 * time.Hour), time.Hour, "too late to create hold"},
		{"has early enough hold on boundary", false, "2", 1, now.Add(time.Hour), time.Hour, "too late to create hold"},
		{"has late enough hold", false, "2", 1, now.Add(-1 * time.Second), time.Hour, "too early to create hold"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := setupEvent(t, now)
			if tt.needsExistingHold {
				setupHold(t, event, now)
			}
			availabilityStart := event.getAvailability()
			holdLenStart := len(event.holds)
			_, got := event.createHold(tt.id, tt.quantity, tt.currentTime, tt.holdDuration)
			if got == nil {
				t.Errorf("createHold wanted error: '%v'", tt.wantErr)
			}
			if got != nil && got.Error() != tt.wantErr {
				t.Errorf("createHold validation error: '%v'; want '%v'", got.Error(), tt.wantErr)
			}
			availabilityEnd := event.getAvailability()
			holdLenEnd := len(event.holds)
			if availabilityStart != availabilityEnd {
				t.Errorf("availability started at %v but ended at %v", availabilityStart, availabilityEnd)
			}
			if holdLenStart != holdLenEnd {
				t.Errorf("hold count started at %v but ended at %v", holdLenStart, holdLenEnd)
			}
		})
	}

	t.Run("creation exactly at onsaleOpening succeeds", func(t *testing.T) {
		now := time.Now()
		event, eventErr := newEvent("1", 1, now, now.Add(time.Minute), now.Add(time.Hour))
		if eventErr != nil {
			t.Fatalf("tried creating a valid event, but got: '%v'", eventErr.Error())
		}
		_, holdErr := event.createHold("1", 1, now, time.Hour)
		if holdErr != nil {
			t.Errorf("expected successful hold creation at onsale opening")
		}
	})

	t.Run("hold duration beyond confirmationCutoff clamps at cutoff", func(t *testing.T) {
		now := time.Now()
		confirmationCutoff := now.Add(time.Hour)
		event, eventErr := newEvent("1", 1, now, now.Add(time.Minute), confirmationCutoff)
		if eventErr != nil {
			t.Fatalf("tried creating a valid event, but got: '%v'", eventErr.Error())
		}
		hold, holdErr := event.createHold("1", 1, now, 2*time.Hour)
		if holdErr != nil {
			t.Fatalf("tried creating a valid hold, but got: '%v'", holdErr.Error())
		}
		if hold.deadline != confirmationCutoff {
			t.Error("expected hold deadline to clamp at confirmation cutoff when hold deadline is longer")
		}
	})
}
