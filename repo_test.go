package rushline

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type operationResult struct {
	hold         Hold
	holdId       string
	operationKey string
	err          error
}

func TestRepository(t *testing.T) {
	t.Run("read hold by event id and hold id", func(t *testing.T) {
		now := time.Now().Truncate(time.Microsecond)
		deadline := now.Add(time.Minute * 30)
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))
		defer cancel()

		repo, _ := setupTestcontainer(t, ctx)

		eventOne, err := repo.CreateEvent(ctx, &Event{
			id:                 "1",
			capacity:           2,
			onsaleOpening:      now.Add(time.Minute),
			holdCreationCutoff: now.Add(time.Hour),
			confirmationCutoff: now.Add(time.Hour * 2),
		})
		require.NoError(t, err)
		eventTwo, err := repo.CreateEvent(ctx, &Event{
			id:                 "2",
			capacity:           2,
			onsaleOpening:      now.Add(time.Minute),
			holdCreationCutoff: now.Add(time.Hour),
			confirmationCutoff: now.Add(time.Hour * 2),
		})
		require.NoError(t, err)
		holdOne, err := repo.CreateHold(ctx, Hold{
			id:       "1",
			status:   HoldStatusExpired,
			quantity: 1,
			deadline: deadline.Add(time.Second),
		}, eventOne.id)
		require.NoError(t, err)
		holdTwo, err := repo.CreateHold(ctx, Hold{
			id:       "1",
			status:   HoldStatusActive,
			quantity: 2,
			deadline: deadline,
		}, eventTwo.id)
		require.NoError(t, err)
		holdThree, err := repo.CreateHold(ctx, Hold{
			id:       "2",
			status:   HoldStatusActive,
			quantity: 2,
			deadline: deadline,
		}, eventOne.id)
		require.NoError(t, err)

		storedHoldOne, err := repo.ReadHold(ctx, holdOne.id, eventOne.id)
		assert.NoError(t, err)
		assert.Equal(t, holdOne, storedHoldOne)
		storedHoldTwo, err := repo.ReadHold(ctx, holdTwo.id, eventTwo.id)
		assert.NoError(t, err)
		assert.Equal(t, holdTwo, storedHoldTwo)

		_, err = repo.ReadHold(ctx, holdThree.id, eventTwo.id)
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("confirm hold", func(t *testing.T) {
		now := time.Now().Truncate(time.Microsecond)
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))
		defer cancel()

		repo, _ := setupTestcontainer(t, ctx)
		event, err := repo.CreateEvent(ctx, &Event{
			id:                 "1",
			capacity:           1,
			onsaleOpening:      now.Add(time.Minute),
			holdCreationCutoff: now.Add(time.Hour),
			confirmationCutoff: now.Add(time.Hour * 2),
		})
		require.NoError(t, err)
		hold, err := repo.CreateHold(ctx, Hold{
			id:       "1",
			status:   HoldStatusActive,
			quantity: 1,
			deadline: now.Add(time.Minute * 30),
		}, event.id)
		require.NoError(t, err)

		updatedHold, err := repo.ConfirmHold(ctx, hold.id, "123", event.id, now)
		assert.NoError(t, err)
		assert.Equal(t, hold.id, updatedHold.id)
		assert.Equal(t, HoldStatusConfirmed, updatedHold.status)
		assert.Equal(t, hold.quantity, updatedHold.quantity)
		assert.Equal(t, hold.deadline, updatedHold.deadline)

		storedHold, err := repo.ReadHold(ctx, hold.id, event.id)
		assert.NoError(t, err)
		assert.Equal(t, updatedHold, storedHold)

		retriedHold, err := repo.ConfirmHold(ctx, hold.id, "123", event.id, now.Add(time.Hour*3))
		assert.NoError(t, err)
		assert.Equal(t, updatedHold, retriedHold)
	})

	t.Run("confirm rollback", func(t *testing.T) {
		now := time.Now().Truncate(time.Microsecond)
		operationKey := "123"
		var pgError *pgconn.PgError
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))
		defer cancel()

		repo, _ := setupTestcontainer(t, ctx)
		event, err := repo.CreateEvent(ctx, &Event{
			id:                 "1",
			capacity:           1,
			onsaleOpening:      now.Add(time.Minute),
			holdCreationCutoff: now.Add(time.Hour),
			confirmationCutoff: now.Add(time.Hour * 2),
		})
		require.NoError(t, err)
		hold, err := repo.CreateHold(ctx, Hold{
			id:       "1",
			status:   HoldStatusActive,
			quantity: 1,
			deadline: now.Add(time.Minute * 30),
		}, event.id)
		require.NoError(t, err)

		_, err = repo.conn.Exec(ctx, "ALTER TABLE operations ADD CONSTRAINT reject_key CHECK (id <> '123')")
		require.NoError(t, err)

		_, err = repo.ConfirmHold(ctx, hold.id, operationKey, event.id, now)
		require.ErrorAs(t, err, &pgError)
		assert.Equal(t, pgError.Code, "23514")
		assert.Equal(t, pgError.ConstraintName, "reject_key")
		assert.Equal(t, pgError.TableName, "operations")

		storedHold, err := repo.ReadHold(ctx, hold.id, event.id)
		assert.NoError(t, err)
		assert.Equal(t, hold, storedHold)

		_, err = repo.ReadOperation(ctx, operationKey, hold.id, event.id)
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("confirm hold status error", func(t *testing.T) {
		now := time.Now().Truncate(time.Microsecond)
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))
		defer cancel()

		repo, _ := setupTestcontainer(t, ctx)
		event, err := repo.CreateEvent(ctx, &Event{
			id:                 "1",
			capacity:           1,
			onsaleOpening:      now.Add(time.Minute),
			holdCreationCutoff: now.Add(time.Hour),
			confirmationCutoff: now.Add(time.Hour * 2),
		})
		require.NoError(t, err)

		tests := []struct {
			name         string
			holdId       string
			operationKey string
			status       HoldStatus
			expectErr    error
		}{
			{"expired", "1", "11", HoldStatusExpired, ErrHoldConfirmExpired},
			{"confirmed", "2", "22", HoldStatusConfirmed, ErrHoldAlreadyConfirmed},
			{"cancelled", "3", "33", HoldStatusCancelled, ErrHoldAlreadyCancelled},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				hold, err := repo.CreateHold(ctx, Hold{
					id:       tt.holdId,
					status:   tt.status,
					quantity: 1,
					deadline: now.Add(time.Minute * 30),
				}, event.id)
				require.NoError(t, err)

				_, err = repo.ConfirmHold(ctx, hold.id, tt.operationKey, event.id, now)
				assert.ErrorIs(t, err, tt.expectErr)

				storedHold, err := repo.ReadHold(ctx, hold.id, event.id)
				assert.NoError(t, err)
				assert.Equal(t, hold, storedHold)

				_, err = repo.ReadOperation(ctx, tt.operationKey, hold.id, event.id)
				assert.ErrorIs(t, err, pgx.ErrNoRows)
			})
		}
	})

	t.Run("confirm hold past deadline", func(t *testing.T) {
		now := time.Now().Truncate(time.Microsecond)
		deadline := now.Add(time.Minute * 30)
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))
		defer cancel()

		repo, _ := setupTestcontainer(t, ctx)
		event, err := repo.CreateEvent(ctx, &Event{
			id:                 "1",
			capacity:           1,
			onsaleOpening:      now.Add(time.Minute),
			holdCreationCutoff: now.Add(time.Hour),
			confirmationCutoff: now.Add(time.Hour * 2),
		})
		require.NoError(t, err)

		tests := []struct {
			name         string
			holdId       string
			operationKey string
			deadline     time.Time
			confirmTime  time.Time
		}{
			{"on hold deadline", "1", "11", deadline, deadline},
			{"after hold deadline", "2", "22", deadline, deadline.Add(time.Minute)},
			{"on confirmation cutoff", "3", "33", deadline.Add(time.Hour * 2), now.Add(time.Hour * 2)},
			{"after confirmation cutoff", "4", "44", deadline.Add(time.Hour * 2), now.Add(time.Hour * 2).Add(time.Minute)},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				hold, err := repo.CreateHold(ctx, Hold{
					id:       tt.holdId,
					status:   HoldStatusActive,
					quantity: 1,
					deadline: tt.deadline,
				}, event.id)
				require.NoError(t, err)

				_, err = repo.ConfirmHold(ctx, hold.id, tt.operationKey, event.id, tt.confirmTime)
				assert.ErrorIs(t, err, ErrAfterConfirmationDeadline)

				storedHold, err := repo.ReadHold(ctx, hold.id, event.id)
				assert.NoError(t, err)
				assert.Equal(t, hold.id, storedHold.id)
				assert.Equal(t, HoldStatusExpired, storedHold.status)
				assert.Equal(t, hold.deadline, storedHold.deadline)
				assert.Equal(t, hold.quantity, storedHold.quantity)

				_, err = repo.ReadOperation(ctx, tt.operationKey, hold.id, event.id)
				assert.ErrorIs(t, err, pgx.ErrNoRows)
			})
		}
	})

	t.Run("confirm hold operation key changed-target conflict", func(t *testing.T) {
		now := time.Now().Truncate(time.Microsecond)
		deadline := now.Add(time.Minute * 30)
		operationKey := "123"
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))
		defer cancel()

		repo, _ := setupTestcontainer(t, ctx)
		event, err := repo.CreateEvent(ctx, &Event{
			id:                 "1",
			capacity:           2,
			onsaleOpening:      now.Add(time.Minute),
			holdCreationCutoff: now.Add(time.Hour),
			confirmationCutoff: now.Add(time.Hour * 2),
		})
		require.NoError(t, err)
		holdOne, err := repo.CreateHold(ctx, Hold{
			id:       "1",
			status:   HoldStatusActive,
			quantity: 1,
			deadline: deadline,
		}, event.id)
		require.NoError(t, err)
		holdOne, err = repo.ConfirmHold(ctx, holdOne.id, operationKey, event.id, now)
		require.NoError(t, err)
		operation, err := repo.ReadOperation(ctx, operationKey, holdOne.id, event.id)
		require.NoError(t, err)

		tests := []struct {
			name        string
			holdId      string
			currentTime time.Time
		}{
			{"before deadline", "2", now},
			{"on deadline", "3", deadline},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				holdTwo, err := repo.CreateHold(ctx, Hold{
					id:       tt.holdId,
					status:   HoldStatusActive,
					quantity: 1,
					deadline: deadline,
				}, event.id)
				require.NoError(t, err)

				_, err = repo.ConfirmHold(ctx, holdTwo.id, operationKey, event.id, tt.currentTime)
				assert.ErrorIs(t, err, ErrOperationKeyConflict)

				storedHoldOne, err := repo.ReadHold(ctx, holdOne.id, event.id)
				assert.NoError(t, err)
				assert.Equal(t, holdOne, storedHoldOne)
				storedHoldTwo, err := repo.ReadHold(ctx, holdTwo.id, event.id)
				assert.NoError(t, err)
				assert.Equal(t, holdTwo, storedHoldTwo)
				storedOperation, err := repo.ReadOperation(ctx, operationKey, holdOne.id, event.id)
				assert.NoError(t, err)
				assert.Equal(t, operation, storedOperation)
			})
		}
	})

	t.Run("confirm hold operation key cross-event reuse", func(t *testing.T) {
		now := time.Now().Truncate(time.Microsecond)
		deadline := now.Add(time.Minute * 30)
		operationKey := "123"
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))
		defer cancel()

		repo, _ := setupTestcontainer(t, ctx)
		eventOne, err := repo.CreateEvent(ctx, &Event{
			id:                 "1",
			capacity:           4,
			onsaleOpening:      now.Add(time.Minute),
			holdCreationCutoff: now.Add(time.Hour),
			confirmationCutoff: now.Add(time.Hour * 2),
		})
		require.NoError(t, err)
		eventTwo, err := repo.CreateEvent(ctx, &Event{
			id:                 "2",
			capacity:           5,
			onsaleOpening:      now.Add(time.Minute),
			holdCreationCutoff: now.Add(time.Hour),
			confirmationCutoff: now.Add(time.Hour * 2),
		})
		require.NoError(t, err)
		holdOne, err := repo.CreateHold(ctx, Hold{
			id:       "1",
			status:   HoldStatusActive,
			quantity: 3,
			deadline: deadline,
		}, eventOne.id)
		require.NoError(t, err)
		holdTwo, err := repo.CreateHold(ctx, Hold{
			id:       "2",
			status:   HoldStatusActive,
			quantity: 5,
			deadline: deadline,
		}, eventTwo.id)
		require.NoError(t, err)

		holdOne, err = repo.ConfirmHold(ctx, holdOne.id, operationKey, eventOne.id, now)
		assert.NoError(t, err)
		holdTwo, err = repo.ConfirmHold(ctx, holdTwo.id, operationKey, eventTwo.id, now)
		assert.NoError(t, err)

		operationOne, err := repo.ReadOperation(ctx, operationKey, holdOne.id, eventOne.id)
		assert.NoError(t, err)
		assert.Equal(t, holdOne, operationOne)
		operationTwo, err := repo.ReadOperation(ctx, operationKey, holdTwo.id, eventTwo.id)
		assert.NoError(t, err)
		assert.Equal(t, holdTwo, operationTwo)
	})

	t.Run("confirm hold empty operation key", func(t *testing.T) {
		now := time.Now().Truncate(time.Microsecond)
		deadline := now.Add(time.Minute * 30)
		operationKey := ""
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))
		defer cancel()

		repo, _ := setupTestcontainer(t, ctx)
		event, err := repo.CreateEvent(ctx, &Event{
			id:                 "1",
			capacity:           10,
			onsaleOpening:      now.Add(time.Minute),
			holdCreationCutoff: now.Add(time.Hour),
			confirmationCutoff: now.Add(time.Hour * 2),
		})
		require.NoError(t, err)

		tests := []struct {
			name        string
			holdId      string
			status      HoldStatus
			currentTime time.Time
		}{
			{"active hold", "1", HoldStatusActive, now},
			{"confirmed hold", "2", HoldStatusConfirmed, now},
			{"cancelled hold", "3", HoldStatusCancelled, now},
			{"overdue hold", "4", HoldStatusActive, deadline},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				hold, err := repo.CreateHold(ctx, Hold{
					id:       tt.holdId,
					status:   tt.status,
					quantity: 3,
					deadline: deadline,
				}, event.id)
				require.NoError(t, err)

				_, err = repo.ConfirmHold(ctx, hold.id, operationKey, event.id, tt.currentTime)
				assert.ErrorIs(t, err, ErrOperationKeyRequired)

				storedHold, err := repo.ReadHold(ctx, hold.id, event.id)
				assert.NoError(t, err)
				assert.Equal(t, hold, storedHold)
			})
		}

		t.Run("nonexistent hold", func(t *testing.T) {
			_, err = repo.ConfirmHold(ctx, "123", operationKey, event.id, now)
			assert.ErrorIs(t, err, ErrOperationKeyRequired)
		})
	})

	t.Run("confirm hold resource not found", func(t *testing.T) {
		now := time.Now().Truncate(time.Microsecond)
		deadline := now.Add(time.Minute * 30)
		operationKey := "123"
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))
		defer cancel()

		repo, _ := setupTestcontainer(t, ctx)
		eventOne, err := repo.CreateEvent(ctx, &Event{
			id:                 "1",
			capacity:           1,
			onsaleOpening:      now.Add(time.Minute),
			holdCreationCutoff: now.Add(time.Hour),
			confirmationCutoff: now.Add(time.Hour * 2),
		})
		require.NoError(t, err)
		_, err = repo.CreateEvent(ctx, &Event{
			id:                 "2",
			capacity:           1,
			onsaleOpening:      now.Add(time.Minute),
			holdCreationCutoff: now.Add(time.Hour),
			confirmationCutoff: now.Add(time.Hour * 2),
		})
		require.NoError(t, err)
		hold, err := repo.CreateHold(ctx, Hold{
			id:       "1",
			status:   HoldStatusActive,
			quantity: 1,
			deadline: deadline,
		}, eventOne.id)
		require.NoError(t, err)

		tests := []struct {
			name      string
			eventId   string
			holdId    string
			expectErr error
		}{
			{"missing hold", "1", "2", ErrHoldNotFound},
			{"missing event", "3", "2", ErrEventNotFound},
			{"hold part of another event", "2", "1", ErrHoldNotFound},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err = repo.ConfirmHold(ctx, tt.holdId, operationKey, tt.eventId, now)
				assert.ErrorIs(t, err, tt.expectErr)

				storedHold, err := repo.ReadHold(ctx, hold.id, eventOne.id)
				assert.NoError(t, err)
				assert.Equal(t, hold, storedHold)

				_, err = repo.ReadOperation(ctx, operationKey, tt.holdId, tt.eventId)
				assert.ErrorIs(t, err, pgx.ErrNoRows)
				_, err = repo.ReadOperation(ctx, operationKey, "1", "1")
				assert.ErrorIs(t, err, pgx.ErrNoRows)
				_, err = repo.ReadOperation(ctx, operationKey, "1", tt.eventId)
				assert.ErrorIs(t, err, pgx.ErrNoRows)
			})
		}
	})

	concurrentTests := []struct {
		name            string
		secondHoldId    string
		operationKeyOne string
		operationKeyTwo string
	}{
		{"same hold, same key", "1", "123", "123"},
		{"same hold, different key", "1", "123", "321"},
		{"different holds, same key", "2", "123", "123"},
	}

	for _, tt := range concurrentTests {
		t.Run(tt.name, func(t *testing.T) {
			holdChan := make(chan operationResult, 2)
			now := time.Now().Truncate(time.Microsecond)
			deadline := now.Add(time.Minute * 30)
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))
			defer cancel()

			connStr, _ := setupContainer(t, ctx)
			repoOne := setupRepository(t, ctx, connStr)
			repoTwo := setupRepository(t, ctx, connStr)
			repoThree := setupRepository(t, ctx, connStr)
			event, err := repoOne.CreateEvent(ctx, &Event{
				id:                 "1",
				capacity:           3,
				onsaleOpening:      now.Add(time.Minute),
				holdCreationCutoff: now.Add(time.Hour),
				confirmationCutoff: now.Add(time.Hour * 2),
			})
			require.NoError(t, err)
			hold, err := repoOne.CreateHold(ctx, Hold{
				id:       "1",
				status:   HoldStatusActive,
				quantity: 1,
				deadline: deadline,
			}, event.id)
			require.NoError(t, err)
			secondHold, err := repoOne.CreateHold(ctx, Hold{
				id:       "2",
				status:   HoldStatusActive,
				quantity: 2,
				deadline: deadline.Add(time.Second),
			}, event.id)
			require.NoError(t, err)

			var pidOne int
			var pidTwo int
			err = repoOne.conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pidOne)
			require.NoError(t, err)
			err = repoTwo.conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pidTwo)
			require.NoError(t, err)
			tx, err := repoThree.conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			require.NoError(t, err)
			defer tx.Rollback(ctx)
			err = tx.QueryRow(ctx,
				"SELECT id FROM events WHERE id = $1 FOR UPDATE",
				event.id).Scan(nil)
			require.NoError(t, err)

			go func() {
				updatedHold, err := repoOne.ConfirmHold(ctx, hold.id, tt.operationKeyOne, event.id, now)
				holdChan <- operationResult{updatedHold, hold.id, tt.operationKeyOne, err}
			}()
			go func() {
				updatedHold, err := repoTwo.ConfirmHold(ctx, tt.secondHoldId, tt.operationKeyTwo, event.id, now)
				holdChan <- operationResult{updatedHold, tt.secondHoldId, tt.operationKeyTwo, err}
			}()

			ticker := time.NewTicker(time.Millisecond * 10)
			defer ticker.Stop()
			for range ticker.C {
				var blockingPidsOne []int64
				var blockingPidsTwo []int64
				err = tx.QueryRow(ctx, "SELECT pg_blocking_pids($1)", pidOne).Scan(&blockingPidsOne)
				require.NoError(t, err)
				err = tx.QueryRow(ctx, "SELECT pg_blocking_pids($1)", pidTwo).Scan(&blockingPidsTwo)
				require.NoError(t, err)
				if len(blockingPidsOne) != 0 && len(blockingPidsTwo) != 0 {
					break
				}
			}
			err = tx.Rollback(ctx)
			require.NoError(t, err)

			var updatedHold, retriedHold Hold
			var errorResult error
			var successKey, failKey string
			var successCount, failCount int
			for range 2 {
				result := <-holdChan
				if result.err == nil {
					var requestedHold Hold
					switch result.holdId {
					case hold.id:
						requestedHold = hold
					case secondHold.id:
						requestedHold = secondHold
					default:
						assert.Fail(t, "requested id doesn't match either hold")
					}
					assert.Equal(t, requestedHold.id, result.hold.id)
					assert.Equal(t, HoldStatusConfirmed, result.hold.status)
					assert.Equal(t, requestedHold.quantity, result.hold.quantity)
					assert.Equal(t, requestedHold.deadline, result.hold.deadline)
					if successCount == 1 {
						retriedHold = result.hold
						assert.Equal(t, updatedHold, retriedHold)
					} else {
						updatedHold = result.hold
					}
					successKey = result.operationKey
					successCount++
				} else {
					errorResult = result.err
					failKey = result.operationKey
					failCount++
				}
			}

			if tt.secondHoldId == "2" {
				assert.Equal(t, successCount, 1)
				assert.Equal(t, failCount, 1)
				assert.ErrorIs(t, errorResult, ErrOperationKeyConflict)
			} else if tt.operationKeyOne != tt.operationKeyTwo {
				assert.Equal(t, successCount, 1)
				assert.Equal(t, failCount, 1)
				assert.ErrorIs(t, errorResult, ErrHoldAlreadyConfirmed)
			} else {
				assert.Equal(t, successCount, 2)
				assert.Equal(t, failCount, 0)
			}

			var operationCount int
			err = repoOne.conn.QueryRow(ctx,
				"SELECT COUNT(*) FROM operations").Scan(&operationCount)
			assert.NoError(t, err)
			assert.Equal(t, 1, operationCount)

			if tt.secondHoldId == "2" {
				var losingHold Hold
				if updatedHold.id == "1" {
					losingHold = secondHold
				} else {
					losingHold = hold
				}
				storedHold, err := repoOne.ReadHold(ctx, losingHold.id, event.id)
				assert.NoError(t, err)
				assert.Equal(t, losingHold, storedHold)
			}
			storedHold, err := repoOne.ReadHold(ctx, updatedHold.id, event.id)
			assert.NoError(t, err)
			assert.Equal(t, updatedHold, storedHold)

			operation, err := repoOne.ReadOperation(ctx, successKey, updatedHold.id, event.id)
			assert.NoError(t, err)
			assert.Equal(t, updatedHold, operation)

			if tt.operationKeyOne != tt.operationKeyTwo {
				_, err := repoOne.ReadOperation(ctx, failKey, updatedHold.id, event.id)
				assert.ErrorIs(t, err, pgx.ErrNoRows)
			}
		})
	}

	t.Run("confirm replay after postgres restart", func(t *testing.T) {
		now := time.Now().Truncate(time.Microsecond)
		deadline := now.Add(time.Minute * 30)
		operationKey := "123"
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(5*time.Second))
		defer cancel()

		repo, pgContainer := setupTestcontainer(t, ctx)
		event, err := repo.CreateEvent(ctx, &Event{
			id:                 "1",
			capacity:           1,
			onsaleOpening:      now.Add(time.Minute),
			holdCreationCutoff: now.Add(time.Hour),
			confirmationCutoff: now.Add(time.Hour * 2),
		})
		require.NoError(t, err)
		hold, err := repo.CreateHold(ctx, Hold{
			id:       "1",
			status:   HoldStatusActive,
			quantity: 1,
			deadline: deadline,
		}, event.id)
		require.NoError(t, err)

		updatedHold, err := repo.ConfirmHold(ctx, hold.id, operationKey, event.id, now)
		assert.NoError(t, err)
		assert.Equal(t, hold.id, updatedHold.id)
		assert.Equal(t, HoldStatusConfirmed, updatedHold.status)
		assert.Equal(t, hold.quantity, updatedHold.quantity)
		assert.Equal(t, hold.deadline, updatedHold.deadline)

		restartCtx, cancelReadiness := context.WithDeadline(context.Background(), time.Now().Add(20*time.Second))
		defer cancelReadiness()

		timeout := time.Minute
		err = pgContainer.Stop(restartCtx, &timeout)
		require.NoError(t, err)
		err = pgContainer.Start(restartCtx)
		require.NoError(t, err)

		connStr, err := pgContainer.ConnectionString(restartCtx, "sslmode=disable")
		require.NoError(t, err)
		newRepo := reconnectTestcontainer(t, restartCtx, connStr)

		newRepoCtx, cancelNewRepoCtx := context.WithDeadline(context.Background(), time.Now().Add(5*time.Second))
		defer cancelNewRepoCtx()

		storedHold, err := newRepo.ReadHold(newRepoCtx, hold.id, event.id)
		assert.NoError(t, err)
		assert.Equal(t, updatedHold, storedHold)

		operation, err := newRepo.ReadOperation(newRepoCtx, operationKey, updatedHold.id, event.id)
		assert.NoError(t, err)
		assert.Equal(t, updatedHold, operation)

		retriedHold, err := newRepo.ConfirmHold(newRepoCtx, hold.id, operationKey, event.id, deadline)
		assert.NoError(t, err)
		assert.Equal(t, updatedHold, retriedHold)

		retriedHold, err = newRepo.ConfirmHold(newRepoCtx, hold.id, operationKey, event.id, event.confirmationCutoff.Add(time.Second))
		assert.NoError(t, err)
		assert.Equal(t, updatedHold, retriedHold)

		var operationCount int
		err = newRepo.conn.QueryRow(newRepoCtx,
			"SELECT COUNT(*) FROM operations").Scan(&operationCount)
		assert.NoError(t, err)
		assert.Equal(t, 1, operationCount)
	})
}
