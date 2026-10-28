package rushline

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestRepository(t *testing.T) {
	t.Run("read hold by event id and hold id", func(t *testing.T) {
		now := time.Now().Truncate(time.Microsecond)
		deadline := now.Add(time.Minute * 30)
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))
		defer cancel()

		pgContainer, err := postgres.Run(ctx,
			"postgres:18",
			postgres.WithInitScripts(filepath.Join(".", "migrations", "000001_create_initial_schema.sql")),
			postgres.WithDatabase("rushline"),
			postgres.WithUsername("postgres"),
			postgres.WithPassword("mysecretpass"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).WithStartupTimeout(5*time.Second)),
		)
		if err != nil {
			t.Fatal(err)
		}

		connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
		require.NoError(t, err)

		repo, err := NewRepository(ctx, connStr)
		require.NoError(t, err)

		t.Cleanup(func() {
			cleanupCtx := context.Background()
			if err := repo.conn.Close(cleanupCtx); err != nil {
				t.Fatalf("failed to close connection: %s", err)
			}
			if err = pgContainer.Terminate(cleanupCtx); err != nil {
				t.Fatalf("failed to terminate pgContainer: %s", err)
			}
		})

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
}
