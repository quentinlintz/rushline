package rushline

import (
	"math"
	"slices"
	"strconv"
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

func TestNewEvent(t *testing.T) {
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
	t.Run("creates valid hold", func(t *testing.T) {
		event := setupEvent(t, now, 5)
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
		if availability != event.capacity-hold.quantity {
			t.Errorf("expected availability == %v, but got: %v", event.capacity-hold.quantity, availability)
		}
		assertExpirableHoldMembership(t, event)
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
			event := setupEvent(t, now, 5)
			if tt.needsExistingHold {
				setupHold(t, event, now)
			}
			availabilityStart := event.getAvailability()
			holdLenStart := len(event.holds)
			expirableHoldsStart := slices.Clone(event.expirableHolds)
			_, got := event.createHold(tt.id, tt.quantity, tt.currentTime, tt.holdDuration)
			if got == nil {
				t.Errorf("createHold wanted error: '%v'", tt.wantErr)
			}
			if got != nil && got.Error() != tt.wantErr {
				t.Errorf("createHold validation error: '%v'; want '%v'", got.Error(), tt.wantErr)
			}
			availabilityEnd := event.getAvailability()
			holdLenEnd := len(event.holds)
			expirableHoldsEnd := event.expirableHolds
			if availabilityStart != availabilityEnd {
				t.Errorf("availability started at %v but ended at %v", availabilityStart, availabilityEnd)
			}
			if holdLenStart != holdLenEnd {
				t.Errorf("hold count started at %v but ended at %v", holdLenStart, holdLenEnd)
			}
			if !slices.Equal(expirableHoldsStart, expirableHoldsEnd) {
				t.Errorf("expected expirableHolds to remain unchanged, but got: %+v", expirableHoldsEnd)
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
		holdChan := make(chan holdResult)
		event, eventErr := newEvent("1", 1, now, now.Add(time.Minute), now.Add(time.Hour))
		if eventErr != nil {
			t.Fatalf("tried creating a valid event, but got: '%v'", eventErr.Error())
		}
		go func() {
			hold, err := event.createHold("1", 1, now, 2*time.Hour)
			holdChan <- holdResult{hold, err}
		}()
		go func() {
			hold, err := event.createHold("2", 1, now, 2*time.Hour)
			holdChan <- holdResult{hold, err}
		}()

		errorCount := 0
		successCount := 0
		for range 2 {
			result := <-holdChan
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
			t.Fatalf("expected 1 success creating a hold, but received: %v", successCount)
		}
		if errorCount != 1 {
			t.Fatalf("expected 1 error creating a hold, but received: %v", errorCount)
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

	t.Run("expirableHolds rollback", func(t *testing.T) {
		expected := "hold with id '1' not found when inserting into expirableHolds"
		event := setupEvent(t, now, 2)
		event.expirableHolds = append(event.expirableHolds, "1")
		availabilityStart := event.getAvailability()
		_, err := event.createHold("2", 1, now, time.Minute)
		_, exists := event.getHoldByID("2")
		availabilityEnd := event.getAvailability()
		if err == nil {
			t.Fatal("expected error for missing id in expirableHolds")
		}
		if err.Error() != expected {
			t.Errorf("expected error '%v', but got '%v'", expected, err.Error())
		}
		if availabilityStart != availabilityEnd {
			t.Errorf("expected availability to be %v, but it was %v", availabilityStart, availabilityEnd)
		}
		if exists {
			t.Error("expected hold with id '2' not to be in holds map")
		}
		if !slices.Equal(event.expirableHolds, []string{"1"}) {
			t.Errorf("expected expirableHolds to have '1', but it has %+v", event.expirableHolds)
		}
	})
}

func TestConfirmHold(t *testing.T) {
	t.Run("confirms valid hold", func(t *testing.T) {
		id := "1"
		event := setupEvent(t, now, 5)
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
		assertExpirableHoldMembership(t, event)
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
			event := setupEvent(t, now, 5)
			hold := setupHold(t, event, now)
			hold.status = tt.status
			if tt.status != HoldStatusActive {
				err := event.removeExpirableHold(tt.id)
				if err != nil {
					t.Fatal(err)
				}
			}
			event.holds[tt.id] = hold
			assertExpirableHoldMembership(t, event)
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
			assertExpirableHoldMembership(t, event)
		})
	}

	t.Run("hold not found", func(t *testing.T) {
		event := setupEvent(t, now, 5)
		_, got := event.confirmHold("1", now)
		wantErr := "hold not found"
		if got == nil {
			t.Errorf("Wanted error: '%v'", wantErr)
		}
		if got != nil && got.Error() != wantErr {
			t.Errorf("expected error '%v', but received: '%v'", wantErr, got)
		}
	})

	expirableTests := []struct {
		name             string
		confirmationTime time.Time
	}{
		{"missing in expirableHolds before deadline", now.Add(time.Second)},
		{"missing in expirableHolds on deadline", now.Add(time.Minute)},
	}

	for _, tt := range expirableTests {
		t.Run(tt.name, func(t *testing.T) {
			expected := "hold id '1' not found when removing from expirableHolds"
			event := setupEvent(t, now, 2)
			holdBefore := setupHold(t, event, now)
			event.expirableHolds = []string{}
			availabilityStart := event.getAvailability()
			_, err := event.confirmHold(holdBefore.id, tt.confirmationTime)
			holdAfter, _ := event.getHoldByID("1")
			availabilityEnd := event.getAvailability()
			if err == nil {
				t.Fatal("expected error when removing missing hold from expirableHolds")
			}
			if err.Error() != expected {
				t.Errorf("expected error to be '%v', but it was '%v'", expected, err.Error())
			}
			if availabilityStart != availabilityEnd {
				t.Errorf("expected availability to be %v, but it was %v", availabilityStart, availabilityEnd)
			}
			if len(event.expirableHolds) != 0 {
				t.Errorf("expected expirableHolds to be empty, but it was %v", len(event.expirableHolds))
			}
			if holdBefore != holdAfter {
				t.Errorf("expected hold to be %+v, but it was %+v", holdBefore, holdAfter)
			}
		})
	}
}

func TestCancelHold(t *testing.T) {
	t.Run("cancels valid hold", func(t *testing.T) {
		id := "1"
		event := setupEvent(t, now, 5)
		hold := setupHold(t, event, now)
		availabilityStart := event.getAvailability()
		assertExpirableHoldMembership(t, event)
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
		assertExpirableHoldMembership(t, event)
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
			event := setupEvent(t, now, 5)
			hold := setupHold(t, event, now)
			hold.status = tt.status
			if tt.status != HoldStatusActive {
				err := event.removeExpirableHold(tt.id)
				if err != nil {
					t.Fatal(err)
				}
			}
			event.holds[tt.id] = hold
			assertExpirableHoldMembership(t, event)
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
			assertExpirableHoldMembership(t, event)
		})
	}

	t.Run("hold not found", func(t *testing.T) {
		event := setupEvent(t, now, 5)
		_, got := event.cancelHold("1", now)
		wantErr := "hold not found"
		if got == nil {
			t.Errorf("Wanted error: '%v'", wantErr)
		}
		if got != nil && got.Error() != wantErr {
			t.Errorf("expected error '%v', but received: '%v'", wantErr, got)
		}
	})

	expirableTests := []struct {
		name             string
		cancellationTime time.Time
	}{
		{"missing in expirableHolds before deadline", now.Add(time.Second)},
		{"missing in expirableHolds on deadline", now.Add(time.Minute)},
	}

	for _, tt := range expirableTests {
		t.Run(tt.name, func(t *testing.T) {
			expected := "hold id '1' not found when removing from expirableHolds"
			event := setupEvent(t, now, 2)
			holdBefore := setupHold(t, event, now)
			event.expirableHolds = []string{}
			availabilityStart := event.getAvailability()
			_, err := event.cancelHold(holdBefore.id, tt.cancellationTime)
			holdAfter, _ := event.getHoldByID("1")
			availabilityEnd := event.getAvailability()
			if err == nil {
				t.Fatal("expected error when removing missing hold from expirableHolds")
			}
			if err.Error() != expected {
				t.Errorf("expected error to be '%v', but it was '%v'", expected, err.Error())
			}
			if availabilityStart != availabilityEnd {
				t.Errorf("expected availability to be %v, but it was %v", availabilityStart, availabilityEnd)
			}
			if len(event.expirableHolds) != 0 {
				t.Errorf("expected expirableHolds to be empty, but it was %v", len(event.expirableHolds))
			}
			if holdBefore != holdAfter {
				t.Errorf("expected hold to be %+v, but it was %+v", holdBefore, holdAfter)
			}
		})
	}
}

func TestExpireHold(t *testing.T) {
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
			event := setupEvent(t, now, 5)
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
			if len(event.expirableHolds) != 0 {
				t.Errorf("expected expired hold with id '%v' to have been removed from expirableHolds", hold.id)
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
			event := setupEvent(t, now, 5)
			hold := setupHold(t, event, now)
			hold.status = tt.status
			if tt.status != HoldStatusActive {
				err := event.removeExpirableHold(tt.id)
				if err != nil {
					t.Fatal(err)
				}
			}
			event.holds[tt.id] = hold
			assertExpirableHoldMembership(t, event)
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
			assertExpirableHoldMembership(t, event)
		})
	}

	t.Run("hold not found", func(t *testing.T) {
		event := setupEvent(t, now, 5)
		_, got := event.expireHold("1", now)
		wantErr := "hold not found"
		if got == nil {
			t.Errorf("Wanted error: '%v'", wantErr)
		}
		if got != nil && got.Error() != wantErr {
			t.Errorf("expected error '%v', but received: '%v'", wantErr, got)
		}
	})

	t.Run("missing in expirableHolds on deadline", func(t *testing.T) {
		expected := "hold id '1' not found when removing from expirableHolds"
		event := setupEvent(t, now, 2)
		holdBefore := setupHold(t, event, now)
		event.expirableHolds = []string{}
		availabilityStart := event.getAvailability()
		_, err := event.expireHold(holdBefore.id, now.Add(time.Minute))
		holdAfter, _ := event.getHoldByID("1")
		availabilityEnd := event.getAvailability()
		if err == nil {
			t.Fatal("expected error when removing missing hold from expirableHolds")
		}
		if err.Error() != expected {
			t.Errorf("expected error to be '%v', but it was '%v'", expected, err.Error())
		}
		if availabilityStart != availabilityEnd {
			t.Errorf("expected availability to be %v, but it was %v", availabilityStart, availabilityEnd)
		}
		if len(event.expirableHolds) != 0 {
			t.Errorf("expected expirableHolds to be empty, but it was %v", len(event.expirableHolds))
		}
		if holdBefore != holdAfter {
			t.Errorf("expected hold to be %+v, but it was %+v", holdBefore, holdAfter)
		}
	})
}

func TestConfirmCancelHoldConcurrent(t *testing.T) {
	t.Run("handles data race for confirming and cancelling the same active hold", func(t *testing.T) {
		holdChan := make(chan holdResult)
		event := setupEvent(t, now, 5)
		hold := setupHold(t, event, now)
		availabilityStart := event.getAvailability()

		go func() {
			hold, err := event.cancelHold("1", now)
			holdChan <- holdResult{hold, err}
		}()
		go func() {
			hold, err := event.confirmHold("1", now)
			holdChan <- holdResult{hold, err}
		}()

		successfulHold := Hold{}
		var failedError error
		successCount := 0
		errorCount := 0
		for range 2 {
			result := <-holdChan
			if result.err == nil {
				successfulHold = result.hold
				successCount++
				continue
			}
			if result.err.Error() != "cannot cancel hold of status 'confirmed'" && result.err.Error() != "cannot confirm hold of status 'cancelled'" {
				t.Errorf("expected cannot cancel/confirm error, but received: '%v'", result.err.Error())
			}
			failedError = result.err
			errorCount++
		}

		if successCount != 1 {
			t.Fatalf("expected 1 success transitioning a hold, but received: %v", successCount)
		}
		if errorCount != 1 {
			t.Fatalf("expected 1 error transitioning a hold, but received: %v", errorCount)
		}

		availabilityEnd := event.getAvailability()
		holdLen := len(event.holds)
		storedHold, exists := event.getHoldByID("1")
		if !exists {
			t.Fatal("expected hold to be found")
		}
		if storedHold != successfulHold {
			t.Error("successful hold does not equal the stored hold")
		}
		if hold.id != storedHold.id {
			t.Errorf("expected hold id to be %v, but got %v", hold.id, storedHold.id)
		}
		if hold.deadline != storedHold.deadline {
			t.Errorf("expected hold deadline to be %v, but got %v", hold.deadline, storedHold.deadline)
		}
		if hold.quantity != storedHold.quantity {
			t.Errorf("expected hold quantity to be %v, but got %v", hold.quantity, storedHold.quantity)
		}

		switch storedHold.status {
		case HoldStatusConfirmed:
			if availabilityStart != availabilityEnd {
				t.Errorf("expected %v available holds, but got %v", availabilityStart, availabilityEnd)
			}
			if failedError.Error() != "cannot cancel hold of status 'confirmed'" {
				t.Errorf("expected error: cannot cancel hold of status 'confirmed', but got %v", failedError.Error())
			}
		case HoldStatusCancelled:
			if availabilityStart+hold.quantity != availabilityEnd {
				t.Errorf("expected %v available holds, but got %v", availabilityStart+hold.quantity, availabilityEnd)
			}
			if failedError.Error() != "cannot confirm hold of status 'cancelled'" {
				t.Errorf("expected error: cannot confirm hold of status 'cancelled', but got %v", failedError.Error())
			}
		default:
			t.Errorf("stored hold has invalid status: '%v'", storedHold.status)
		}
		if holdLen != 1 {
			t.Errorf("expected 1 count holds, but got %v", holdLen)
		}
	})
}

func TestConfirmExpireHoldConcurrent(t *testing.T) {
	holdChan := make(chan holdResult)
	event := setupEvent(t, now, 5)
	hold := setupHold(t, event, now)
	availabilityStart := event.getAvailability()

	go func() {
		hold, err := event.confirmHold("1", hold.deadline)
		holdChan <- holdResult{hold, err}
	}()
	go func() {
		hold, err := event.expireHold("1", hold.deadline)
		holdChan <- holdResult{hold, err}
	}()

	successfulHold := hold
	errorSlice := make([]string, 0, 2)
	successCount := 0
	for range 2 {
		result := <-holdChan
		if result.err == nil {
			successfulHold = result.hold
			successCount++
			continue
		}
		errorSlice = append(errorSlice, result.err.Error())
	}

	availabilityEnd := event.getAvailability()
	storedHold, exists := event.getHoldByID("1")
	if !exists {
		t.Fatal("expected hold to be found")
	}
	switch len(errorSlice) {
	// Expiration first
	case 1:
		if !slices.Contains(errorSlice, "cannot confirm hold of status 'expired'") {
			t.Error("expected error: cannot confirm hold of status 'expired'")
		}
		if successCount != 1 {
			t.Errorf("expected 1 success, but got %v", successCount)
		}
		if storedHold != successfulHold {
			t.Error("successful hold does not equal the stored hold")
		}
	// Confirmation first
	case 2:
		if !slices.Contains(errorSlice, "can only transition hold before confirmation deadline") {
			t.Error("expected error: can only transition hold before confirmation deadline")
		}
		if !slices.Contains(errorSlice, "hold is already expired") {
			t.Error("expected error: hold is already expired")
		}
	default:
		t.Fatalf("expected 1 or 2 errors, but got %v", len(errorSlice))
	}
	if hold.id != storedHold.id {
		t.Errorf("expected hold id to be %v, but got %v", hold.id, storedHold.id)
	}
	if hold.deadline != storedHold.deadline {
		t.Errorf("expected hold deadline to be %v, but got %v", hold.deadline, storedHold.deadline)
	}
	if hold.quantity != storedHold.quantity {
		t.Errorf("expected hold quantity to be %v, but got %v", hold.quantity, storedHold.quantity)
	}
	if storedHold.status != HoldStatusExpired {
		t.Errorf("expected status to be '4', but got '%v'", storedHold.status)
	}
	if availabilityStart != availabilityEnd-hold.quantity {
		t.Errorf("expected availability to increase by hold quantity, got start %v and end %v available", availabilityStart, availabilityEnd)
	}
}

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
	})

	t.Run("no eligible holds", func(t *testing.T) {
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
}

func BenchmarkScanForExpireHolds(b *testing.B) {
	tests := []struct {
		name       string
		totalHolds int
		dueHolds   int
	}{
		{"1_000 holds, 10 due", 1_000, 10},
		{"10_000 holds, 10 due", 10_000, 10},
		{"100_000 holds, 10 due", 100_000, 10},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			expiredHoldStartIdx := tt.totalHolds - tt.dueHolds
			event := setupEvent(b, now, tt.totalHolds)
			for i := range tt.totalHolds {
				var err error
				if i < expiredHoldStartIdx {
					_, err = event.createHold(strconv.Itoa(i), 1, now, time.Minute)
				} else {
					_, err = event.createHold(strconv.Itoa(i), 1, now, time.Second)
				}
				if err != nil {
					b.Fatalf("failed to create hold with id %v", strconv.Itoa(i))
				}
			}

			for b.Loop() {
				err := event.scanForExpiredHolds(now.Add(time.Second))
				b.StopTimer()
				if err != nil {
					b.Fatalf("failure from scan: %v", err.Error())
				}
				for i := range tt.totalHolds {
					idx := strconv.Itoa(i)
					hold, exists := event.getHoldByID(idx)
					if !exists {
						b.Fatalf("expected hold with id %v to exist", idx)
					}
					if i < expiredHoldStartIdx {
						if hold.status != HoldStatusActive {
							b.Errorf("expected hold with id %v to be active, but got '%v'", hold.id, hold.status)
						}
					} else {
						if hold.status != HoldStatusExpired {
							b.Errorf("expected hold with id %v to be expired, but got '%v'", hold.id, hold.status)
						}
						hold.status = HoldStatusActive
						event.holds[idx] = hold
					}
				}
				b.StartTimer()
			}
		})
	}
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
