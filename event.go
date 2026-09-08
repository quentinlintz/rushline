package rushline

import (
	"errors"
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

	id       string
	capacity int
	holds    map[string]Hold

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
			hold.status = HoldStatusExpired
			e.holds[id] = hold
			return Hold{}, errors.New("can only transition hold before confirmation deadline")
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

func (e *Event) getHoldByIDLocked(id string) (Hold, bool) {
	hold, ok := e.holds[id]
	return hold, ok
}

func (e *Event) getHoldByID(id string) (Hold, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.getHoldByIDLocked(id)
}
