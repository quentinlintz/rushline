package rushline

import (
	"strconv"
	"testing"
	"time"
)

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
				id := strconv.Itoa(i)
				if i < expiredHoldStartIdx {
					event.holds[id] = Hold{id: id, status: HoldStatusActive, quantity: 1, deadline: now.Add(time.Minute)}
				} else {
					event.holds[id] = Hold{id: id, status: HoldStatusActive, quantity: 1, deadline: now.Add(time.Second)}
					event.expirableHolds = append(event.expirableHolds, id)
				}
			}

			for i := range expiredHoldStartIdx {
				id := strconv.Itoa(i)
				event.expirableHolds = append(event.expirableHolds, id)
			}

			if len(event.expirableHolds) != tt.totalHolds {
				b.Fatalf("expected %v expirableHolds, but got %v", tt.totalHolds, len(event.expirableHolds))
			}
			if len(event.holds) != tt.totalHolds {
				b.Fatalf("expected %v holds, but got %v", tt.totalHolds, len(event.holds))
			}
			lastDeadline := now
			dueHoldsCount := 0
			seenIds := make(map[string]struct{})
			for i := range tt.totalHolds {
				id := event.expirableHolds[i]
				_, exists := seenIds[id]
				if exists {
					b.Fatalf("id '%v' already exists in holds", id)
				}
				hold, exists := event.getHoldByID(id)
				if !exists {
					b.Fatalf("hold with id '%v' doesn't exist", id)
				}
				if hold.status != HoldStatusActive {
					b.Fatalf("expected hold with id '%v' to be active, but it was '%v'", id, hold.status)
				}
				if lastDeadline.After(hold.deadline) {
					b.Fatalf("expected hold deadline order to be correct")
				}
				if hold.deadline.Equal(now.Add(time.Second)) || hold.deadline.Before(now.Add(time.Second)) {
					dueHoldsCount++
				}
				lastDeadline = hold.deadline
				seenIds[id] = struct{}{}
			}
			if dueHoldsCount != tt.dueHolds {
				b.Fatalf("expected %v holds due, but got %v", tt.dueHolds, dueHoldsCount)
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
							b.Fatalf("expected hold with id %v to be active, but got '%v'", hold.id, hold.status)
						}
					} else {
						if hold.status != HoldStatusExpired {
							b.Fatalf("expected hold with id %v to be expired, but got '%v'", hold.id, hold.status)
						}
						hold.status = HoldStatusActive
						event.holds[idx] = hold
						err := event.insertExpirableHold(hold)
						if err != nil {
							b.Fatalf("failed to insert id of '%v' from expirableHolds: %v", idx, err.Error())
						}
					}
				}
				b.StartTimer()
			}
		})
	}
}
