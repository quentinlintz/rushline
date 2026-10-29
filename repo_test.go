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

func TestRepository(t *testing.T) {
	t.Run("read hold by event id and hold id", func(t *testing.T) {
		now := time.Now().Truncate(time.Microsecond)
		deadline := now.Add(time.Minute * 30)
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))
		defer cancel()

		repo := setupTestcontainer(t, ctx)

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

		repo := setupTestcontainer(t, ctx)
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

		repo := setupTestcontainer(t, ctx)
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

		repo := setupTestcontainer(t, ctx)
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

		repo := setupTestcontainer(t, ctx)
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

		repo := setupTestcontainer(t, ctx)
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

		repo := setupTestcontainer(t, ctx)
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

		repo := setupTestcontainer(t, ctx)
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
}
