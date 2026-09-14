package rushline

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

type HoldStatus int

const (
	HoldStatusInvalid   HoldStatus = 0
	HoldStatusActive    HoldStatus = 1
	HoldStatusConfirmed HoldStatus = 2
	HoldStatusCancelled HoldStatus = 3
	HoldStatusExpired   HoldStatus = 4
)

type Hold struct {
	id       string
	status   HoldStatus
	quantity int

	deadline time.Time
}

type Event struct {
	mu sync.Mutex

	id             string
	capacity       int
	holds          map[string]Hold
	expirableHolds []string // Active holds ordered by deadline

	onsaleOpening      time.Time
	holdCreationCutoff time.Time
	confirmationCutoff time.Time
}

func newEvent(id string, capacity int, onsaleOpening, holdCreationCutoff, confirmationCutoff time.Time) (*Event, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}

	if capacity <= 0 {
		return nil, errors.New("must request positive capacity")
	}

	if onsaleOpening.After(holdCreationCutoff) || onsaleOpening.Equal(holdCreationCutoff) {
		return nil, errors.New("hold creation cutoff must be after onsale opening")
	}

	if holdCreationCutoff.After(confirmationCutoff) || holdCreationCutoff.Equal(confirmationCutoff) {
		return nil, errors.New("confirmation must be after hold creation cutoff")
	}

	newEvent := &Event{
		id:                 id,
		capacity:           capacity,
		holds:              make(map[string]Hold),
		onsaleOpening:      onsaleOpening,
		holdCreationCutoff: holdCreationCutoff,
		confirmationCutoff: confirmationCutoff,
	}

	return newEvent, nil
}

func (e *Event) createHold(id string, quantity int, currentTime time.Time, holdDuration time.Duration) (Hold, error) {
	if holdDuration <= 0 {
		return Hold{}, errors.New("holdDuration must be in the future")
	}

	if id == "" {
		return Hold{}, errors.New("id is required")
	}

	if quantity <= 0 {
		return Hold{}, errors.New("must request positive quantity")
	}

	if e.holdCreationCutoff.Before(currentTime) || e.holdCreationCutoff.Equal(currentTime) {
		return Hold{}, errors.New("too late to create hold")
	}

	if e.onsaleOpening.After(currentTime) {
		return Hold{}, errors.New("too early to create hold")
	}

	confirmationDeadline := currentTime.Add(holdDuration)
	if e.confirmationCutoff.Before(confirmationDeadline) {
		confirmationDeadline = e.confirmationCutoff
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	_, ok := e.getHoldByIDLocked(id)
	if ok {
		return Hold{}, errors.New("id is duplicate")
	}

	if quantity > e.getAvailabilityLocked() {
		return Hold{}, errors.New("cannot over-sell")
	}

	newHold := Hold{
		id:       id,
		status:   HoldStatusActive,
		quantity: quantity,
		deadline: confirmationDeadline,
	}
	e.holds[newHold.id] = newHold
	err := e.insertExpirableHoldLocked(newHold)
	if err != nil {
		delete(e.holds, newHold.id)
		return Hold{}, err
	}

	return newHold, nil
}

func (e *Event) confirmHold(id string, currentTime time.Time) (Hold, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.confirmHoldLocked(id, currentTime)
}

func (e *Event) confirmHoldLocked(id string, currentTime time.Time) (Hold, error) {
	hold, ok := e.getHoldByIDLocked(id)

	if !ok {
		return Hold{}, errors.New("hold not found")
	}

	switch hold.status {
	case HoldStatusInvalid:
		return Hold{}, errors.New("cannot confirm hold of status 'invalid'")
	case HoldStatusActive:
		if currentTime.After(hold.deadline) || currentTime.Equal(hold.deadline) {
			err := e.removeExpirableHoldLocked(id)
			if err != nil {
				return Hold{}, err
			}
			hold.status = HoldStatusExpired
			e.holds[id] = hold
			return Hold{}, errors.New("can only transition hold before confirmation deadline")
		}
		err := e.removeExpirableHoldLocked(id)
		if err != nil {
			return Hold{}, err
		}
		hold.status = HoldStatusConfirmed
		e.holds[id] = hold
		return e.holds[id], nil
	case HoldStatusConfirmed:
		return Hold{}, errors.New("hold is already confirmed")
	case HoldStatusCancelled:
		return Hold{}, errors.New("cannot confirm hold of status 'cancelled'")
	case HoldStatusExpired:
		return Hold{}, errors.New("cannot confirm hold of status 'expired'")
	default:
		return Hold{}, errors.New("hold has unknown status")
	}
}

func (e *Event) cancelHold(id string, currentTime time.Time) (Hold, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cancelHoldLocked(id, currentTime)
}

func (e *Event) cancelHoldLocked(id string, currentTime time.Time) (Hold, error) {
	hold, ok := e.getHoldByIDLocked(id)

	if !ok {
		return Hold{}, errors.New("hold not found")
	}

	switch hold.status {
	case HoldStatusInvalid:
		return Hold{}, errors.New("cannot cancel hold of status 'invalid'")
	case HoldStatusActive:
		if currentTime.After(hold.deadline) || currentTime.Equal(hold.deadline) {
			err := e.removeExpirableHoldLocked(id)
			if err != nil {
				return Hold{}, err
			}
			hold.status = HoldStatusExpired
			e.holds[id] = hold
			return Hold{}, errors.New("can only transition hold before confirmation deadline")
		}
		err := e.removeExpirableHoldLocked(id)
		if err != nil {
			return Hold{}, err
		}
		hold.status = HoldStatusCancelled
		e.holds[id] = hold
		return e.holds[id], nil
	case HoldStatusConfirmed:
		return Hold{}, errors.New("cannot cancel hold of status 'confirmed'")
	case HoldStatusCancelled:
		return Hold{}, errors.New("hold is already cancelled")
	case HoldStatusExpired:
		return Hold{}, errors.New("cannot cancel hold of status 'expired'")
	default:
		return Hold{}, errors.New("hold has unknown status")
	}
}

func (e *Event) expireHold(id string, currentTime time.Time) (Hold, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.expireHoldLocked(id, currentTime)
}

func (e *Event) expireHoldLocked(id string, currentTime time.Time) (Hold, error) {
	hold, ok := e.getHoldByIDLocked(id)

	if !ok {
		return Hold{}, errors.New("hold not found")
	}

	switch hold.status {
	case HoldStatusInvalid:
		return Hold{}, errors.New("cannot expire hold of status 'invalid'")
	case HoldStatusActive:
		if currentTime.Before(hold.deadline) {
			return Hold{}, errors.New("can only expire holds at or after the deadline")
		}
		err := e.removeExpirableHoldLocked(id)
		if err != nil {
			return Hold{}, err
		}
		hold.status = HoldStatusExpired
		e.holds[id] = hold
		return hold, nil
	case HoldStatusConfirmed:
		return Hold{}, errors.New("cannot expire hold of status 'confirmed'")
	case HoldStatusCancelled:
		return Hold{}, errors.New("cannot expire hold of status 'cancelled'")
	case HoldStatusExpired:
		return Hold{}, errors.New("hold is already expired")
	default:
		return Hold{}, errors.New("hold has unknown status")
	}
}

func (e *Event) scanForExpiredHolds(currentTime time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, hold := range e.holds {
		if hold.status == HoldStatusActive && (currentTime.After(hold.deadline) || currentTime.Equal(hold.deadline)) {
			_, err := e.expireHoldLocked(id, currentTime)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Event) getAvailabilityLocked() int {
	return e.capacity - e.getTotalHoldsByStatusLocked(HoldStatusActive) - e.getTotalHoldsByStatusLocked(HoldStatusConfirmed)
}

func (e *Event) getAvailability() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.getAvailabilityLocked()
}

func (e *Event) getTotalHoldsByStatusLocked(status HoldStatus) int {
	total := 0
	for _, hold := range e.holds {
		if hold.status == status {
			total += hold.quantity
		}
	}
	return total
}

func (e *Event) getHoldByID(id string) (Hold, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.getHoldByIDLocked(id)
}

func (e *Event) getHoldByIDLocked(id string) (Hold, bool) {
	hold, ok := e.holds[id]
	return hold, ok
}

func (e *Event) insertExpirableHold(hold Hold) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.insertExpirableHoldLocked(hold)
}

func (e *Event) insertExpirableHoldLocked(hold Hold) error {
	expHoldLength := len(e.expirableHolds)
	storedHold, exists := e.getHoldByIDLocked(hold.id)
	if !exists {
		return fmt.Errorf("hold not found when inserting into expirableHolds")
	}
	if slices.Contains(e.expirableHolds, hold.id) {
		return fmt.Errorf("hold with id '%v' already exists in expirableHolds", hold.id)
	}

	for i := range expHoldLength {
		storedHold, exists = e.getHoldByIDLocked(e.expirableHolds[i])
		if !exists {
			return fmt.Errorf("hold with id '%v' not found when inserting into expirableHolds", e.expirableHolds[i])
		}
		if storedHold.deadline.After(hold.deadline) {
			e.expirableHolds = slices.Insert(e.expirableHolds, i, hold.id)
			return nil
		}
	}
	e.expirableHolds = append(e.expirableHolds, hold.id)
	return nil
}

func (e *Event) removeExpirableHold(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.removeExpirableHoldLocked(id)
}

func (e *Event) removeExpirableHoldLocked(id string) error {
	for i := range len(e.expirableHolds) {
		if id == e.expirableHolds[i] {
			e.expirableHolds = slices.Delete(e.expirableHolds, i, i+1)
			return nil
		}
	}
	return fmt.Errorf("hold id '%v' not found when removing from expirableHolds", id)
}
