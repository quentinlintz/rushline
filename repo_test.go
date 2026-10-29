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

		storedHoldOne, err := repo.GetHoldById(ctx, holdOne.id, eventOne.id)
		assert.NoError(t, err)
		assert.Equal(t, holdOne, storedHoldOne)
		storedHoldTwo, err := repo.GetHoldById(ctx, holdTwo.id, eventTwo.id)
		assert.NoError(t, err)
		assert.Equal(t, holdTwo, storedHoldTwo)

		_, err = repo.GetHoldById(ctx, holdThree.id, eventTwo.id)
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("confirm hold", func(t *testing.T) {
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
		hold, err := repo.CreateHold(ctx, Hold{
			id:       "1",
			status:   HoldStatusActive,
			quantity: 1,
			deadline: deadline.Add(time.Second),
		}, event.id)
		require.NoError(t, err)

		updatedHold, err := repo.UpdateHoldStatusById(ctx, hold.id, "123", event.id, HoldStatusConfirmed)
		assert.NoError(t, err)
		assert.Equal(t, hold.id, updatedHold.id)
		assert.Equal(t, HoldStatusConfirmed, updatedHold.status)
		assert.Equal(t, hold.quantity, updatedHold.quantity)
		assert.Equal(t, hold.deadline, updatedHold.deadline)

		storedHold, err := repo.GetHoldById(ctx, hold.id, event.id)
		assert.NoError(t, err)
		assert.Equal(t, updatedHold, storedHold)

		retriedHold, err := repo.UpdateHoldStatusById(ctx, hold.id, "123", event.id, HoldStatusConfirmed)
		assert.NoError(t, err)
		assert.Equal(t, updatedHold, retriedHold)
	})

	t.Run("confirm rollback", func(t *testing.T) {
		now := time.Now().Truncate(time.Microsecond)
		deadline := now.Add(time.Minute * 30)
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
			deadline: deadline.Add(time.Second),
		}, event.id)
		require.NoError(t, err)

		_, err = repo.conn.Exec(ctx, "ALTER TABLE operations ADD CONSTRAINT reject_key CHECK (id <> '123')")
		require.NoError(t, err)

		_, err = repo.UpdateHoldStatusById(ctx, hold.id, operationKey, event.id, HoldStatusConfirmed)
		require.ErrorAs(t, err, &pgError)
		assert.Equal(t, pgError.Code, "23514")
		assert.Equal(t, pgError.ConstraintName, "reject_key")
		assert.Equal(t, pgError.TableName, "operations")

		storedHold, err := repo.GetHoldById(ctx, hold.id, event.id)
		assert.NoError(t, err)
		assert.Equal(t, hold, storedHold)

		_, err = repo.GetHoldOperation(ctx, operationKey, hold.id, event.id)
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})
}
