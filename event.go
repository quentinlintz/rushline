package rushline

import (
	"errors"
	"sync"
	"time"
)

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

func (e *Event) getAvailability() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.getAvailabilityLocked()
}

func (e *Event) getAvailabilityLocked() int {
	return e.capacity - e.getTotalHoldsByStatusLocked(HoldStatusActive) - e.getTotalHoldsByStatusLocked(HoldStatusConfirmed)
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
