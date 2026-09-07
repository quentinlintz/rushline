package rushline

import (
	"errors"
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
	id       string
	capacity int
	holds    []Hold

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

	if e.getHoldByID(id).status != HoldStatusInvalid {
		return Hold{}, errors.New("id is duplicate")
	}

	if quantity <= 0 {
		return Hold{}, errors.New("must request positive quantity")
	}

	if quantity > e.getAvailability() {
		return Hold{}, errors.New("cannot over-sell")
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

	newHold := Hold{
		id:       id,
		status:   HoldStatusActive,
		quantity: quantity,
		deadline: confirmationDeadline,
	}
	e.holds = append(e.holds, newHold)

	return newHold, nil
}

func (e Event) getAvailability() int {
	return e.capacity - e.getTotalHoldsByStatus(HoldStatusActive) - e.getTotalHoldsByStatus(HoldStatusConfirmed)
}

func (e Event) getTotalHoldsByStatus(status HoldStatus) int {
	total := 0
	for _, hold := range e.holds {
		if hold.status == status {
			total += hold.quantity
		}
	}
	return total
}

func (e Event) getHoldByID(id string) Hold {
	for _, hold := range e.holds {
		if hold.id == id {
			return hold
		}
	}
	return Hold{}
}
