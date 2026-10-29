package rushline

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

type holdResult struct {
	hold Hold
	err  error
}

var now = time.Unix(1781622600, 0)

func setupEvent(tb testing.TB, now time.Time, capacity int) *Event {
	tb.Helper()
	event, err := newEvent("1", capacity, now, now.Add(time.Hour), now.Add(time.Hour+time.Minute))
	if err != nil {
		tb.Fatalf("tried creating a valid event, but got: '%v'", err.Error())
	}
	return event
}

func setupHold(t *testing.T, event *Event, now time.Time) Hold {
	t.Helper()
	hold, err := event.createHold("1", 1, now, time.Minute)
	if err != nil {
		t.Fatalf("tried creating a valid hold, but got: '%v'", err.Error())
	}
	return hold
}

func countOccurrences[T comparable](slice []T, target T) int {
	count := 0
	for _, v := range slice {
		if v == target {
			count++
		}
	}
	return count
}

func assertExpirableHoldMembership(t *testing.T, e *Event) {
	t.Helper()
	for id := range e.holds {
		occurrences := countOccurrences(e.expirableHolds, id)
		if e.holds[id].status == HoldStatusActive {
			if occurrences == 0 {
				t.Errorf("expected hold with id '%v' to be in expirableHolds exactly once, but not found", id)
			}
			if occurrences > 1 {
				t.Errorf("expected hold with id '%v' to be in expirableHolds exactly once, but contained %v", id, occurrences)
			}
		} else {
			if occurrences != 0 {
				t.Errorf("expected hold with id '%v' to not be found in expirableHolds, but contained %v", id, occurrences)
			}
		}
	}
	for _, id := range e.expirableHolds {
		_, exists := e.getHoldByID(id)
		if !exists {
			t.Errorf("expected expirableHold entry with id '%v' to be in holds map", id)
		}
	}
}

func setupTestcontainer(t *testing.T, ctx context.Context) *Repository {
	pgContainer, err := postgres.Run(ctx,
		"postgres:18",
		postgres.WithInitScripts(filepath.Join(".", "migrations", "000001_create_initial_schema.sql")),
		postgres.WithDatabase("rushline"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("mysecretpass"),
		testcontainers.WithWaitStrategyAndDeadline(
			time.Minute,
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	if pgContainer == nil {
		t.Fatalf("pgContainer is nil: %s", err)
	}
	if err != nil {
		cleanupCtx, cancelCleanup := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))
		defer cancelCleanup()
		if termErr := pgContainer.Terminate(cleanupCtx); termErr != nil {
			t.Errorf("failed to terminate pgContainer: %s", termErr)
		}
		t.Fatal(err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))
		defer cancelCleanup()
		if err := pgContainer.Terminate(cleanupCtx); err != nil {
			t.Fatalf("failed to terminate pgContainer: %s", err)
		}
	})

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	repo, err := NewRepository(ctx, connStr)
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))
		defer cancelCleanup()
		if err := repo.conn.Close(cleanupCtx); err != nil {
			t.Fatalf("failed to close connection: %s", err)
		}
	})

	return repo
}
