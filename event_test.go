package rushline

import (
	"math"
	"testing"
	"time"
)

type createHoldResult struct {
	hold Hold
	err  error
}

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
	now := time.Unix(1781622600, 0)

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
	now := time.Unix(1781622600, 0)

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

	t.Run("handles data race for holding over capacity", func(t *testing.T) {
		holdResult := make(chan createHoldResult)
		event, eventErr := newEvent("1", 1, now, now.Add(time.Minute), now.Add(time.Hour))
		if eventErr != nil {
			t.Fatalf("tried creating a valid event, but got: '%v'", eventErr.Error())
		}
		go func() {
			hold, err := event.createHold("1", 1, now, 2*time.Hour)
			holdResult <- createHoldResult{hold, err}
		}()
		go func() {
			hold, err := event.createHold("2", 1, now, 2*time.Hour)
			holdResult <- createHoldResult{hold, err}
		}()

		errorCount := 0
		successCount := 0
		for range 2 {
			result := <-holdResult
			if result.err == nil {
				if result.hold.id != "1" && result.hold.id != "2" {
					t.Error("expected hold ids to match '1' or '2'")
				}
				successCount++
				continue
			}
			if result.err.Error() != "cannot over-sell" {
				t.Errorf("expected error 'cannot over-sell', but received: %v", result.err.Error())
			}
			errorCount++
		}

		if successCount != 1 {
			t.Errorf("expected 1 success creating a hold, but received: %v", successCount)
		}
		if errorCount != 1 {
			t.Errorf("expected 1 error creating a hold, but received: %v", errorCount)
		}

		availability := event.getAvailability()
		holdLen := len(event.holds)
		if availability != 0 {
			t.Errorf("expected 0 available holds, but got %v", availability)
		}
		if holdLen != 1 {
			t.Errorf("expected 1 count holds, but got %v", holdLen)
		}
	})
}

func TestConfirmHold(t *testing.T) {
	now := time.Unix(1781622600, 0)

	t.Run("confirms valid hold", func(t *testing.T) {
		id := "1"
		event := setupEvent(t, now)
		hold := setupHold(t, event, now)
		availabilityStart := event.getAvailability()
		confirmedHold, err := event.confirmHold(id, now)
		if err != nil {
			t.Fatalf("confirmHold returned error: '%v'", err)
		}

		availabilityEnd := event.getAvailability()
		storedHold, _ := event.getHoldByID(id)
		if storedHold.status != HoldStatusConfirmed {
			t.Errorf("expected confirmed hold, got status: '%v'", storedHold.status)
		}
		if availabilityStart != availabilityEnd {
			t.Errorf("expected availability to not change, got start %v and end %v available", availabilityStart, availabilityEnd)
		}
		if storedHold != confirmedHold {
			t.Errorf("expected returned hold to equal stored hold, got stored '%v' and returned '%v'", storedHold, confirmedHold)
		}
		if hold.id != confirmedHold.id {
			t.Errorf("expected hold id to not have changed, got original '%v' and returned '%v'", hold.id, confirmedHold.id)
		}
		if hold.quantity != confirmedHold.quantity {
			t.Errorf("expected hold quantity to not have changed, got original '%v' and returned '%v'", hold.quantity, confirmedHold.quantity)
		}
		if hold.deadline != confirmedHold.deadline {
			t.Errorf("expected hold deadline to not have changed, got original '%v' and returned '%v'", hold.deadline, confirmedHold.deadline)
		}
	})

	tests := []struct {
		name            string
		id              string
		status          HoldStatus
		endStatus       HoldStatus
		endAvailability int
		currentTime     time.Time
		wantErr         string
	}{
		{"has unknown status", "1", 123, 123, 5, now, "hold has unknown status"},
		{"has invalid status", "1", HoldStatusInvalid, HoldStatusInvalid, 5, now, "cannot confirm hold of status 'invalid'"},
		{"has cancelled status", "1", HoldStatusCancelled, HoldStatusCancelled, 5, now, "cannot confirm hold of status 'cancelled'"},
		{"has expired status", "1", HoldStatusExpired, HoldStatusExpired, 5, now, "cannot confirm hold of status 'expired'"},
		{"hold confirmed after deadline", "1", HoldStatusActive, HoldStatusExpired, 5, now.Add(2 * time.Hour), "can only transition hold before confirmation deadline"},
		{"has confirmed on deadline", "1", HoldStatusActive, HoldStatusExpired, 5, now.Add(time.Minute), "can only transition hold before confirmation deadline"},
		{"has already confirmed", "1", HoldStatusConfirmed, HoldStatusConfirmed, 4, now, "hold is already confirmed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := setupEvent(t, now)
			hold := setupHold(t, event, now)
			hold.status = tt.status
			event.holds[tt.id] = hold
			_, err := event.confirmHold(tt.id, tt.currentTime)
			storedHold, found := event.getHoldByID(tt.id)
			availabilityEnd := event.getAvailability()
			if !found {
				t.Fatal("hold not found after confirmHold")
			}
			if err == nil {
				t.Errorf("confirmHold wanted error: '%v'", tt.wantErr)
			}
			if err != nil && err.Error() != tt.wantErr {
				t.Errorf("confirmHold error: '%v'; want '%v'", err.Error(), tt.wantErr)
			}
			if storedHold.status != tt.endStatus {
				t.Errorf("expected status to change to '%v', but got '%v'", tt.endStatus, storedHold.status)
			}
			if availabilityEnd != tt.endAvailability {
				t.Errorf("expected availability to be %v, but got %v", tt.endAvailability, availabilityEnd)
			}
			if hold.id != storedHold.id {
				t.Errorf("expected hold id to not have changed, got original '%v' and returned '%v'", hold.id, storedHold.id)
			}
			if hold.quantity != storedHold.quantity {
				t.Errorf("expected hold quantity to not have changed, got original '%v' and returned '%v'", hold.quantity, storedHold.quantity)
			}
			if hold.deadline != storedHold.deadline {
				t.Errorf("expected hold deadline to not have changed, got original '%v' and returned '%v'", hold.deadline, storedHold.deadline)
			}
		})
	}

	t.Run("hold not found", func(t *testing.T) {
		event := setupEvent(t, now)
		_, got := event.confirmHold("1", now)
		wantErr := "hold not found"
		if got == nil {
			t.Errorf("Wanted error: '%v'", wantErr)
		}
		if got != nil && got.Error() != wantErr {
			t.Errorf("expected error '%v', but received: '%v'", wantErr, got)
		}
	})
}

func TestCancelHold(t *testing.T) {
	now := time.Unix(1781622600, 0)

	t.Run("cancels valid hold", func(t *testing.T) {
		id := "1"
		event := setupEvent(t, now)
		hold := setupHold(t, event, now)
		availabilityStart := event.getAvailability()
		cancelledHold, err := event.cancelHold(id, now)
		if err != nil {
			t.Fatalf("cancelHold returned error: '%v'", err)
		}

		availabilityEnd := event.getAvailability()
		storedHold, _ := event.getHoldByID(id)
		if storedHold.status != HoldStatusCancelled {
			t.Errorf("expected cancelled hold, got status: '%v'", storedHold.status)
		}
		if availabilityStart != availabilityEnd-hold.quantity {
			t.Errorf("expected availability to increase by hold quantity, got start %v and end %v available", availabilityStart, availabilityEnd)
		}
		if storedHold != cancelledHold {
			t.Errorf("expected returned hold to equal stored hold, got stored '%v' and returned '%v'", storedHold, cancelledHold)
		}
		if hold.id != cancelledHold.id {
			t.Errorf("expected hold id to not have changed, got original '%v' and returned '%v'", hold.id, cancelledHold.id)
		}
		if hold.quantity != cancelledHold.quantity {
			t.Errorf("expected hold quantity to not have changed, got original '%v' and returned '%v'", hold.quantity, cancelledHold.quantity)
		}
		if hold.deadline != cancelledHold.deadline {
			t.Errorf("expected hold deadline to not have changed, got original '%v' and returned '%v'", hold.deadline, cancelledHold.deadline)
		}
	})

	tests := []struct {
		name            string
		id              string
		status          HoldStatus
		endStatus       HoldStatus
		endAvailability int
		currentTime     time.Time
		wantErr         string
	}{
		{"has unknown status", "1", 123, 123, 5, now, "hold has unknown status"},
		{"has invalid status", "1", HoldStatusInvalid, HoldStatusInvalid, 5, now, "cannot cancel hold of status 'invalid'"},
		{"has confirmed status", "1", HoldStatusConfirmed, HoldStatusConfirmed, 4, now, "cannot cancel hold of status 'confirmed'"},
		{"has expired status", "1", HoldStatusExpired, HoldStatusExpired, 5, now, "cannot cancel hold of status 'expired'"},
		{"has cancelled after deadline", "1", HoldStatusActive, HoldStatusExpired, 5, now.Add(2 * time.Hour), "can only transition hold before confirmation deadline"},
		{"has cancelled on deadline", "1", HoldStatusActive, HoldStatusExpired, 5, now.Add(time.Minute), "can only transition hold before confirmation deadline"},
		{"has already cancelled", "1", HoldStatusCancelled, HoldStatusCancelled, 5, now, "hold is already cancelled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := setupEvent(t, now)
			hold := setupHold(t, event, now)
			hold.status = tt.status
			event.holds[tt.id] = hold
			_, err := event.cancelHold(tt.id, tt.currentTime)
			storedHold, found := event.getHoldByID(tt.id)
			availabilityEnd := event.getAvailability()
			if !found {
				t.Fatal("hold not found after cancelHold")
			}
			if err == nil {
				t.Errorf("cancelHold wanted error: '%v'", tt.wantErr)
			}
			if err != nil && err.Error() != tt.wantErr {
				t.Errorf("cancelHold error: '%v'; want '%v'", err.Error(), tt.wantErr)
			}
			if storedHold.status != tt.endStatus {
				t.Errorf("expected status to change to '%v', but got '%v'", tt.endStatus, storedHold.status)
			}
			if availabilityEnd != tt.endAvailability {
				t.Errorf("expected availability to be %v, but got %v", tt.endAvailability, availabilityEnd)
			}
			if hold.id != storedHold.id {
				t.Errorf("expected hold id to not have changed, got original '%v' and returned '%v'", hold.id, storedHold.id)
			}
			if hold.quantity != storedHold.quantity {
				t.Errorf("expected hold quantity to not have changed, got original '%v' and returned '%v'", hold.quantity, storedHold.quantity)
			}
			if hold.deadline != storedHold.deadline {
				t.Errorf("expected hold deadline to not have changed, got original '%v' and returned '%v'", hold.deadline, storedHold.deadline)
			}
		})
	}

	t.Run("hold not found", func(t *testing.T) {
		event := setupEvent(t, now)
		_, got := event.cancelHold("1", now)
		wantErr := "hold not found"
		if got == nil {
			t.Errorf("Wanted error: '%v'", wantErr)
		}
		if got != nil && got.Error() != wantErr {
			t.Errorf("expected error '%v', but received: '%v'", wantErr, got)
		}
	})
}

func TestExpireHold(t *testing.T) {
	now := time.Unix(1781622600, 0)

	validTests := []struct {
		name        string
		currentTime time.Time
	}{
		{"expires valid hold on deadline", now.Add(time.Minute)},
		{"expires valid hold after deadline", now.Add(2 * time.Minute)},
	}
	for _, tt := range validTests {
		t.Run(tt.name, func(t *testing.T) {
			id := "1"
			event := setupEvent(t, now)
			hold := setupHold(t, event, now)
			availabilityStart := event.getAvailability()
			expiredHold, err := event.expireHold(id, tt.currentTime)
			if err != nil {
				t.Fatalf("expireHold returned error: '%v'", err)
			}

			availabilityEnd := event.getAvailability()
			storedHold, _ := event.getHoldByID(id)
			if storedHold.status != HoldStatusExpired {
				t.Errorf("expected expired hold, got status: '%v'", storedHold.status)
			}
			if availabilityStart != availabilityEnd-hold.quantity {
				t.Errorf("expected availability to increase by hold quantity, got start %v and end %v available", availabilityStart, availabilityEnd)
			}
			if storedHold != expiredHold {
				t.Errorf("expected returned hold to equal stored hold, got stored '%v' and returned '%v'", storedHold, expiredHold)
			}
			if hold.id != expiredHold.id {
				t.Errorf("expected hold id to not have changed, got original '%v' and returned '%v'", hold.id, expiredHold.id)
			}
			if hold.quantity != expiredHold.quantity {
				t.Errorf("expected hold quantity to not have changed, got original '%v' and returned '%v'", hold.quantity, expiredHold.quantity)
			}
			if hold.deadline != expiredHold.deadline {
				t.Errorf("expected hold deadline to not have changed, got original '%v' and returned '%v'", hold.deadline, expiredHold.deadline)
			}
		})
	}

	tests := []struct {
		name            string
		id              string
		status          HoldStatus
		endStatus       HoldStatus
		endAvailability int
		currentTime     time.Time
		wantErr         string
	}{
		{"has unknown status", "1", 123, 123, 5, now, "hold has unknown status"},
		{"has invalid status", "1", HoldStatusInvalid, HoldStatusInvalid, 5, now, "cannot expire hold of status 'invalid'"},
		{"has confirmed status", "1", HoldStatusConfirmed, HoldStatusConfirmed, 4, now, "cannot expire hold of status 'confirmed'"},
		{"has expired status", "1", HoldStatusExpired, HoldStatusExpired, 5, now, "hold is already expired"},
		{"has already cancelled", "1", HoldStatusCancelled, HoldStatusCancelled, 5, now, "cannot expire hold of status 'cancelled'"},
		{"before the deadline", "1", HoldStatusActive, HoldStatusActive, 4, now.Add(time.Second), "can only expire holds at or after the deadline"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := setupEvent(t, now)
			hold := setupHold(t, event, now)
			hold.status = tt.status
			event.holds[tt.id] = hold
			_, err := event.expireHold(tt.id, tt.currentTime)
			storedHold, found := event.getHoldByID(tt.id)
			availabilityEnd := event.getAvailability()
			if !found {
				t.Fatal("hold not found after expireHold")
			}
			if err == nil {
				t.Errorf("expireHold wanted error: '%v'", tt.wantErr)
			}
			if err != nil && err.Error() != tt.wantErr {
				t.Errorf("expireHold error: '%v'; want '%v'", err.Error(), tt.wantErr)
			}
			if storedHold.status != tt.endStatus {
				t.Errorf("expected status to change to '%v', but got '%v'", tt.endStatus, storedHold.status)
			}
			if availabilityEnd != tt.endAvailability {
				t.Errorf("expected availability to be %v, but got %v", tt.endAvailability, availabilityEnd)
			}
			if hold.id != storedHold.id {
				t.Errorf("expected hold id to not have changed, got original '%v' and returned '%v'", hold.id, storedHold.id)
			}
			if hold.quantity != storedHold.quantity {
				t.Errorf("expected hold quantity to not have changed, got original '%v' and returned '%v'", hold.quantity, storedHold.quantity)
			}
			if hold.deadline != storedHold.deadline {
				t.Errorf("expected hold deadline to not have changed, got original '%v' and returned '%v'", hold.deadline, storedHold.deadline)
			}
		})
	}

	t.Run("hold not found", func(t *testing.T) {
		event := setupEvent(t, now)
		_, got := event.expireHold("1", now)
		wantErr := "hold not found"
		if got == nil {
			t.Errorf("Wanted error: '%v'", wantErr)
		}
		if got != nil && got.Error() != wantErr {
			t.Errorf("expected error '%v', but received: '%v'", wantErr, got)
		}
	})
}
