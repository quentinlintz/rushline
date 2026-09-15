package rushline

import (
	"slices"
	"strconv"
	"testing"
	"time"
)

func TestScanForExpireHolds(t *testing.T) {
	t.Run("event with variety of holds", func(t *testing.T) {
		holds := []struct {
			id             string
			quantity       int
			currentTime    time.Time
			holdDuration   time.Duration
			expectedStatus HoldStatus
		}{
			{"1", 2, now, time.Second, HoldStatusExpired},
			{"2", 1, now.Add(time.Minute), time.Minute, HoldStatusCancelled},
			{"3", 2, now.Add(time.Minute), time.Minute, HoldStatusConfirmed},
			{"4", 1, now, time.Minute, HoldStatusExpired},
			{"5", 2, now, time.Minute, HoldStatusExpired},
			{"6", 1, now.Add(time.Minute), time.Minute, HoldStatusActive},
		}

		event := setupEvent(t, now, 9)
		originalHolds := make(map[string]Hold)
		for _, hold := range holds {
			originalHold, err := event.createHold(hold.id, hold.quantity, hold.currentTime, hold.holdDuration)
			if err != nil {
				t.Fatalf("error when creating hold of id '%v': %v", hold.id, err)
			}
			originalHolds[originalHold.id] = originalHold
		}

		if event.getAvailability() != 0 {
			t.Fatalf("expected availability to be 0, but got: %v", event.getAvailability())
		}
		for id, hold := range event.holds {
			if hold.status != HoldStatusActive {
				t.Fatalf("expected hold with id '%v' to have active status", id)
			}
		}

		_, err := event.cancelHold("2", now.Add(time.Minute))
		if err != nil {
			t.Fatalf("cancelling hold failed: %v", err.Error())
		}
		_, err = event.confirmHold("3", now.Add(time.Minute))
		if err != nil {
			t.Fatalf("confirming hold failed: %v", err.Error())
		}
		_, err = event.expireHold("4", now.Add(time.Minute))
		if err != nil {
			t.Fatalf("expiring hold failed: %v", err.Error())
		}

		if event.getAvailability() != 2 {
			t.Fatalf("expected availability to be 2, but got: %v", event.getAvailability())
		}
		for range 2 {
			expectedExpirableHolds := []string{"6"}
			scanErr := event.scanForExpiredHolds(now.Add(time.Minute))
			if scanErr != nil {
				t.Fatalf("scanForExpiredHolds failed: %v", scanErr.Error())
			}
			if event.getAvailability() != 6 {
				t.Errorf("expected availability to be 6, but got: %v", event.getAvailability())
			}
			if len(event.holds) != 6 {
				t.Errorf("expected hold count to be 6, but got: %v", len(event.holds))
			}
			if !slices.Equal(expectedExpirableHolds, event.expirableHolds) {
				t.Errorf("expected expirableHolds to be '%+v', but got '%+v'", expectedExpirableHolds, event.expirableHolds)
			}

			for _, hold := range holds {
				storedHold, exists := event.getHoldByID(hold.id)
				if !exists {
					t.Errorf("expected hold with id %v to exist", hold.id)
					continue
				}
				copyHold := originalHolds[hold.id]
				copyHold.status = hold.expectedStatus
				if copyHold != storedHold {
					t.Errorf("expected hold with id %v to be %+v, but got %+v", hold.id, copyHold, storedHold)
				}
			}
		}
	})

	t.Run("empty event", func(t *testing.T) {
		event := setupEvent(t, now, 5)

		if event.getAvailability() != 5 {
			t.Fatalf("starting availability of an empty event should be 5, but got: %v", event.getAvailability())
		}
		scanErr := event.scanForExpiredHolds(now.Add(time.Minute))
		if scanErr != nil {
			t.Fatalf("scanForExpiredHolds failed: %v", scanErr.Error())
		}
		if event.getAvailability() != 5 {
			t.Errorf("expected ending availability to be 5, but got: %v", event.getAvailability())
		}
		if len(event.holds) != 0 {
			t.Errorf("expected holds to be empty, but got: %v", len(event.holds))
		}
		if len(event.expirableHolds) != 0 {
			t.Errorf("expected expirableHolds to be empty, but got: %v", len(event.expirableHolds))
		}
	})

	t.Run("no eligible holds", func(t *testing.T) {
		expectedExpirableHolds := []string{"1"}
		holds := []struct {
			id           string
			quantity     int
			currentTime  time.Time
			holdDuration time.Duration
		}{
			{"1", 1, now, time.Hour},
			{"2", 1, now, time.Second},
			{"3", 1, now, time.Second},
			{"4", 1, now, time.Second},
			{"5", 1, now, time.Minute},
			{"6", 1, now, time.Minute},
			{"7", 1, now, time.Minute},
		}

		event, err := newEvent("1", 7, now, now.Add(time.Hour), now.Add(time.Hour+time.Minute))
		if err != nil {
			t.Fatalf("tried creating a valid event, but got: '%v'", err.Error())
		}
		for _, hold := range holds {
			_, err := event.createHold(hold.id, hold.quantity, hold.currentTime, hold.holdDuration)
			if err != nil {
				t.Fatalf("error when creating hold of id '%v': %v", hold.id, err)
			}
		}

		_, err = event.cancelHold("2", now)
		if err != nil {
			t.Fatalf("cancelling hold failed: %v", err.Error())
		}
		_, err = event.confirmHold("3", now)
		if err != nil {
			t.Fatalf("confirming hold failed: %v", err.Error())
		}
		_, err = event.expireHold("4", now.Add(time.Minute))
		if err != nil {
			t.Fatalf("expiring hold failed: %v", err.Error())
		}
		_, err = event.cancelHold("5", now)
		if err != nil {
			t.Fatalf("cancelling hold failed: %v", err.Error())
		}
		_, err = event.confirmHold("6", now)
		if err != nil {
			t.Fatalf("confirming hold failed: %v", err.Error())
		}
		_, err = event.expireHold("7", now.Add(time.Minute))
		if err != nil {
			t.Fatalf("expiring hold failed: %v", err.Error())
		}

		if event.getAvailability() != 4 {
			t.Errorf("expected availability to be 4, but got: %v", event.getAvailability())
		}
		holdsBefore := make(map[string]Hold)
		for _, hold := range holds {
			h, exists := event.getHoldByID(hold.id)
			if !exists {
				t.Fatalf("expected hold with id %v to exist", hold.id)
			}
			holdsBefore[hold.id] = h
		}
		scanErr := event.scanForExpiredHolds(now.Add(time.Minute))
		if scanErr != nil {
			t.Fatalf("scanForExpiredHolds failed: %v", scanErr.Error())
		}
		if len(holdsBefore) != len(event.holds) {
			t.Fatalf("expected hold count to be %v but got: %v", len(holdsBefore), len(event.holds))
		}
		if event.getAvailability() != 4 {
			t.Errorf("expected availability to be 4, but got: %v", event.getAvailability())
		}
		if !slices.Equal(expectedExpirableHolds, event.expirableHolds) {
			t.Errorf("expected expirableHolds to be '%+v', but got '%+v'", expectedExpirableHolds, event.expirableHolds)
		}
		for _, hold := range holdsBefore {
			storedHold, exists := event.getHoldByID(hold.id)
			if !exists {
				t.Errorf("expected hold with id %v to exist after sweep", hold.id)
				continue
			}
			if storedHold != hold {
				t.Errorf("expected hold with id %v to be %+v, but got %+v", hold.id, hold, storedHold)
			}
		}
	})

	holds := []Hold{
		{id: "1", quantity: 1, status: HoldStatusExpired, deadline: now.Add(time.Second)},
		{id: "2", quantity: 1, status: HoldStatusExpired, deadline: now.Add(time.Minute)},
		{id: "3", quantity: 1, status: HoldStatusExpired, deadline: now.Add(time.Second)},
	}
	durations := []time.Duration{time.Second, time.Minute, time.Second}

	t.Run("all holds due", func(t *testing.T) {
		event := setupEvent(t, now, 3)
		for i, hold := range holds {
			_, err := event.createHold(hold.id, hold.quantity, now, durations[i])
			if err != nil {
				t.Fatalf("error when creating hold of id '%v': %v", hold.id, err)
			}
		}
		if !slices.Equal(event.expirableHolds, []string{holds[0].id, holds[2].id, holds[1].id}) {
			t.Fatalf("expirableHolds doesn't contain hold ids after creating holds, contains: %+v", event.expirableHolds)
		}
		err := event.scanForExpiredHolds(now.Add(time.Minute))
		if err != nil {
			t.Fatalf("expected successful scanForExpiredHolds, but got error '%v'", err.Error())
		}
		if len(event.expirableHolds) != 0 {
			t.Errorf("expected expirableHolds to be empty, but it was %+v", event.expirableHolds)
		}
		if event.getAvailability() != 3 {
			t.Errorf("expected availability to be 3, but it was %v", event.getAvailability())
		}

		for _, hold := range holds {
			storedHold := event.holds[hold.id]
			if storedHold != hold {
				t.Errorf("expected hold to be '%+v' in map, but it was '%+v'", hold, storedHold)
			}
		}
	})

	t.Run("missing id", func(t *testing.T) {
		expectedError := "hold with id '2' not found in holds map"
		event := setupEvent(t, now, 2)
		hold := setupHold(t, event, now)
		event.expirableHolds = []string{"2"}
		availabilityStart := event.getAvailability()
		err := event.scanForExpiredHolds(now.Add(time.Second))
		availabilityEnd := event.getAvailability()
		if err == nil {
			t.Fatal("expected error from missing hold id in expirableHolds")
		}
		if err.Error() != expectedError {
			t.Errorf("expected error '%v' from scanForExpiredHolds with a missing hold id, but got '%v'", expectedError, err.Error())
		}
		if availabilityStart != availabilityEnd {
			t.Errorf("expected availability to be %v, but it was %v", availabilityStart, availabilityEnd)
		}
		if len(event.holds) != 1 {
			t.Errorf("expected 1 hold to exist in map, but got %v", len(event.holds))
		}
		if event.holds[hold.id] != hold {
			t.Errorf("expected hold with id '%v' to exist in map", hold.id)
		}
		if !slices.Equal(event.expirableHolds, []string{"2"}) {
			t.Errorf("expected expirableHolds to be '2', but got '%v'", event.expirableHolds)
		}
	})
}

func TestInsertExpirableHold(t *testing.T) {
	tests := []struct {
		name     string
		deadline []time.Time
		expected []string
	}{
		{"inserts before", []time.Time{now.Add(2 * time.Minute), now.Add(time.Minute), now}, []string{"3", "2", "1"}},
		{"inserts between", []time.Time{now, now.Add(time.Minute), now.Add(time.Second)}, []string{"1", "3", "2"}},
		{"inserts after", []time.Time{now, now.Add(time.Minute), now.Add(2 * time.Minute)}, []string{"1", "2", "3"}},
		{"inserts equal deadline", []time.Time{now, now, now}, []string{"1", "2", "3"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := setupEvent(t, now, 3)
			for i := range 3 {
				id := strconv.Itoa(i + 1)
				event.holds[id] = Hold{id: id, status: HoldStatusActive, quantity: 1, deadline: tt.deadline[i]}
				err := event.insertExpirableHold(event.holds[id])
				if err != nil {
					t.Fatalf("failed to insert hold with id '%v' into expirableHolds: %v", id, err.Error())
				}
			}
			if !slices.Equal(event.expirableHolds, tt.expected) {
				t.Errorf("expected ids in order '%+v', but got '%+v'", tt.expected, event.expirableHolds)
			}
		})
	}

	t.Run("missing hold id", func(t *testing.T) {
		expected := "hold not found when inserting into expirableHolds"
		event := setupEvent(t, now, 1)
		hold := Hold{id: "1", status: HoldStatusActive, quantity: 1, deadline: now.Add(time.Minute)}
		err := event.insertExpirableHold(hold)
		if err == nil {
			t.Fatalf("expected missing hold id error")
		}
		if err.Error() != expected {
			t.Errorf("expected error '%v', but received '%v'", expected, err.Error())
		}
		if len(event.expirableHolds) != 0 {
			t.Errorf("expected expirableHolds to be empty, but it has %+v", event.expirableHolds)
		}
	})

	t.Run("insert duplicate id", func(t *testing.T) {
		expected := "hold with id '1' already exists in expirableHolds"
		event := setupEvent(t, now, 1)
		event.holds["1"] = Hold{id: "1", status: HoldStatusActive, quantity: 1, deadline: now}
		availabilityStart := event.getAvailability()
		err := event.insertExpirableHold(event.holds["1"])
		if err != nil {
			t.Fatalf("failed to insert hold with id '1' into expirableHolds: %v", err.Error())
		}
		err = event.insertExpirableHold(event.holds["1"])
		availabilityEnd := event.getAvailability()
		if err == nil {
			t.Fatal("expected error when inserting duplicate id into expirableHolds")
		}
		if err.Error() != expected {
			t.Errorf("expected error '%v' when inserting duplicate into expirableHolds, but got '%v'", expected, err.Error())
		}
		if !slices.Equal(event.expirableHolds, []string{"1"}) {
			t.Errorf("expected expirableHolds to be unchanged, but got %+v", event.expirableHolds)
		}
		if availabilityStart != availabilityEnd {
			t.Errorf("expected availability to be %v, but got %v", availabilityStart, availabilityEnd)
		}
	})

	t.Run("hold not found in map", func(t *testing.T) {
		expected := "hold with id '1' not found when inserting into expirableHolds"
		event := setupEvent(t, now, 2)
		hold := Hold{id: "2", status: HoldStatusActive, quantity: 1, deadline: now.Add(time.Minute)}
		event.holds["2"] = hold
		event.expirableHolds = append(event.expirableHolds, "1")
		availabilityStart := event.getAvailability()
		err := event.insertExpirableHold(hold)
		availabilityEnd := event.getAvailability()
		if err == nil {
			t.Fatal("expected error but got none")
		}
		if err.Error() != expected {
			t.Errorf("expected error '%v', but got '%v'", expected, err.Error())
		}
		if !slices.Equal(event.expirableHolds, []string{"1"}) {
			t.Errorf("expected expirableHolds to remain unchanged, but it was '%+v'", event.expirableHolds)
		}
		if availabilityStart != availabilityEnd {
			t.Errorf("expected availability to be %v, but got %v", availabilityStart, availabilityEnd)
		}
	})
}

func TestRemoveExpirableHold(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		expected []string
	}{
		{"remove first", "1", []string{"2", "3"}},
		{"remove last", "3", []string{"1", "2"}},
		{"remove middle", "2", []string{"1", "3"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := setupEvent(t, now, 3)
			for i := range 3 {
				id := strconv.Itoa(i + 1)
				event.holds[id] = Hold{id: id, status: HoldStatusActive, quantity: 1, deadline: now}
				event.expirableHolds = append(event.expirableHolds, id)
			}
			err := event.removeExpirableHold(tt.id)
			if err != nil {
				t.Fatalf("failed to remove hold with id '%v' from expirableHolds: %v", tt.id, err.Error())
			}
			if !slices.Equal(event.expirableHolds, tt.expected) {
				t.Errorf("expected ids in order '%+v', but got '%+v'", tt.expected, event.expirableHolds)
			}
		})
	}

	t.Run("hold id not found", func(t *testing.T) {
		expected := "hold id '2' not found when removing from expirableHolds"
		event := setupEvent(t, now, 2)
		hold := Hold{id: "1", status: HoldStatusActive, quantity: 1, deadline: now}
		event.holds[hold.id] = hold
		event.expirableHolds = append(event.expirableHolds, "1")
		err := event.removeExpirableHold("2")
		if err == nil {
			t.Fatalf("expected missing hold id error")
		}
		if err.Error() != expected {
			t.Errorf("expected error '%v', but received '%v'", expected, err.Error())
		}
		if !slices.Equal(event.expirableHolds, []string{"1"}) {
			t.Errorf("expected expirableHolds to remain unchanged, but it was '%+v'", event.expirableHolds)
		}
	})
}
