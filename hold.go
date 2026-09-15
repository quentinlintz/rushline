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
