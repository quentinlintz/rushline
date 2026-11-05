package rushline

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

type holdResult struct {
	hold Hold
	err  error
}

type AcquireEvent struct {
	Worker string
	PID    uint32
	Err    error
}

var now = time.Unix(1781622600, 0)

type ConnectionAcquireTracer struct {
	EventChan chan<- AcquireEvent
}

func (t *ConnectionAcquireTracer) TraceAcquireStart(ctx context.Context, pool *pgxpool.Pool, data pgxpool.TraceAcquireStartData) context.Context {
	return ctx
}

func (t *ConnectionAcquireTracer) TraceAcquireEnd(ctx context.Context, pool *pgxpool.Pool, data pgxpool.TraceAcquireEndData) {
	val := ctx.Value(workerKey)
	if val == nil {
		return
	}

	worker, ok := val.(string)
	if !ok {
		return
	}

	if data.Err != nil {
		t.EventChan <- AcquireEvent{
			Worker: worker,
			PID:    0,
			Err:    data.Err,
		}
	} else {
		t.EventChan <- AcquireEvent{
			Worker: worker,
			PID:    data.Conn.PgConn().PID(),
			Err:    nil,
		}
	}
}

func (t *ConnectionAcquireTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return ctx
}

func (t *ConnectionAcquireTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
}

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

func setupContainer(t *testing.T, ctx context.Context) (string, *postgres.PostgresContainer) {
	t.Helper()
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

	return connStr, pgContainer
}

func setupRepositoryWithObserver(t *testing.T, ctx context.Context, connStr string, eventChan chan<- AcquireEvent) *Repository {
	t.Helper()
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		require.NoError(t, err)
	}

	config.ConnConfig.Tracer = &ConnectionAcquireTracer{
		EventChan: eventChan,
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		require.NoError(t, err)
	}

	connCtx, cancel := context.WithDeadline(ctx, time.Now().Add(time.Second*5))
	defer cancel()

	if err = pool.Ping(connCtx); err != nil {
		pool.Close()
		require.NoError(t, err)
	}

	repo := &Repository{
		pool: pool,
	}

	t.Cleanup(func() {
		repo.Close()
	})

	return repo
}

func setupRepository(t *testing.T, ctx context.Context, connStr string) *Repository {
	t.Helper()
	repo, err := NewRepository(ctx, connStr)
	require.NoError(t, err)

	t.Cleanup(func() {
		repo.Close()
	})

	return repo
}

func setupTestcontainer(t *testing.T, ctx context.Context) (*Repository, *postgres.PostgresContainer) {
	t.Helper()
	connStr, pgContainer := setupContainer(t, ctx)
	return setupRepository(t, ctx, connStr), pgContainer
}

func reconnectTestcontainer(t *testing.T, ctx context.Context, connStr string) *Repository {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var pool *pgxpool.Pool
	var err error
	for range ticker.C {
		if ctx.Err() != nil {
			require.Fail(t, "failed to reconnect to db in time")
		}
		pool, err = pgxpool.New(ctx, connStr)
		if err != nil {
			continue
		}
		if pool.Ping(ctx) == nil {
			break
		}
		pool.Close()
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return &Repository{
		pool: pool,
	}
}
