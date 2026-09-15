package rushline

import (
	"fmt"
	"slices"
	"time"
)

func (e *Event) scanForExpiredHolds(currentTime time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.scanForExpiredHoldsLocked(currentTime)
}

func (e *Event) scanForExpiredHoldsLocked(currentTime time.Time) error {
	for len(e.expirableHolds) > 0 {
		id := e.expirableHolds[0]
		hold, exists := e.getHoldByIDLocked(id)
		if !exists {
			return fmt.Errorf("hold with id '%v' not found in holds map", id)
		}
		if currentTime.Before(hold.deadline) {
			return nil
		}
		_, err := e.expireHoldLocked(id, currentTime)
		if err != nil {
			return err
		}
	}
	return nil
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
	if len(e.expirableHolds) != 0 && e.expirableHolds[0] == id {
		e.expirableHolds[0] = ""
		e.expirableHolds = e.expirableHolds[1:]
		return nil
	}

	for i := range len(e.expirableHolds) {
		if id == e.expirableHolds[i] {
			e.expirableHolds = slices.Delete(e.expirableHolds, i, i+1)
			return nil
		}
	}
	return fmt.Errorf("hold id '%v' not found when removing from expirableHolds", id)
}
