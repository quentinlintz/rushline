package rushline

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrEventNotFound = errors.New("event not found")

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(ctx context.Context, connStr string) (*Repository, error) {
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, err
	}

	connCtx, cancel := context.WithDeadline(ctx, time.Now().Add(time.Second*5))
	defer cancel()
	if err = pool.Ping(connCtx); err != nil {
		pool.Close()
		return nil, err
	}

	return &Repository{
		pool: pool,
	}, nil
}

func (r Repository) Close() {
	r.pool.Close()
}

func (r Repository) CreateEvent(ctx context.Context, event *Event) (*Event, error) {
	err := r.pool.QueryRow(ctx,
		"INSERT INTO events (id, capacity, onsale_opening, hold_creation_cutoff, confirmation_cutoff) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		event.id, event.capacity, event.onsaleOpening, event.holdCreationCutoff, event.confirmationCutoff).Scan(&event.id)
	return event, err
}

func (r Repository) CreateHold(ctx context.Context, hold Hold, eventId string) (Hold, error) {
	err := r.pool.QueryRow(ctx,
		"INSERT INTO holds (id, status, quantity, deadline, event_id) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		hold.id, hold.status, hold.quantity, hold.deadline, eventId).Scan(&hold.id)
	return hold, err
}

func (r Repository) CreateOperation(ctx context.Context, id string, hold Hold, eventId string) (Hold, error) {
	err := r.pool.QueryRow(ctx,
		"INSERT INTO operations (id, status, quantity, deadline, event_id, hold_id) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id",
		id, hold.status, hold.quantity, hold.deadline, eventId, hold.id).Scan(&id)
	return hold, err
}

func (r Repository) ReadHold(ctx context.Context, id string, eventId string) (Hold, error) {
	var hold Hold
	err := r.pool.QueryRow(ctx,
		"SELECT id, status, quantity, deadline, event_id FROM holds WHERE id = $1 AND event_id = $2",
		id, eventId).Scan(&hold.id, &hold.status, &hold.quantity, &hold.deadline, nil)
	if err != nil {
		return Hold{}, err
	}
	return hold, nil
}

func (r Repository) ReadOperation(ctx context.Context, id string, holdId string, eventId string) (Hold, error) {
	var hold Hold
	err := r.pool.QueryRow(ctx,
		"SELECT hold_id, status, quantity, deadline, event_id FROM operations WHERE id = $1 AND hold_id = $2 AND event_id = $3",
		id, holdId, eventId).Scan(&hold.id, &hold.status, &hold.quantity, &hold.deadline, nil)
	if err != nil {
		return Hold{}, err
	}
	return hold, nil
}

func (r Repository) ConfirmHold(ctx context.Context, id string, operationKey string, eventId string, currentTime time.Time) (Hold, error) {
	if len(operationKey) == 0 {
		return Hold{}, ErrOperationKeyRequired
	}

	var hold Hold
	var eventConfirmationCutoff time.Time
	status := HoldStatusConfirmed
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Hold{}, err
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx,
		"SELECT confirmation_cutoff FROM events WHERE id = $1 FOR UPDATE",
		eventId).Scan(&eventConfirmationCutoff)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Hold{}, ErrEventNotFound
		}
		return Hold{}, err
	}

	err = tx.QueryRow(ctx,
		"SELECT hold_id, status, quantity, deadline FROM operations WHERE id = $1 AND event_id = $2",
		operationKey, eventId).Scan(&hold.id, &hold.status, &hold.quantity, &hold.deadline)
	if err == nil {
		if hold.id == id {
			return hold, nil
		}
		return Hold{}, ErrOperationKeyConflict
	} else if err != pgx.ErrNoRows {
		return Hold{}, err
	}

	err = tx.QueryRow(ctx,
		"SELECT id, status, quantity, deadline, event_id FROM holds WHERE id = $1 AND event_id = $2",
		id, eventId).Scan(&hold.id, &hold.status, &hold.quantity, &hold.deadline, nil)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Hold{}, ErrHoldNotFound
		}
		return Hold{}, err
	}

	switch hold.status {
	case HoldStatusConfirmed:
		return Hold{}, ErrHoldAlreadyConfirmed
	case HoldStatusCancelled:
		return Hold{}, ErrHoldAlreadyCancelled
	case HoldStatusExpired:
		return Hold{}, ErrHoldConfirmExpired
	}

	if currentTime.After(hold.deadline) ||
		currentTime.Equal(hold.deadline) ||
		currentTime.After(eventConfirmationCutoff) ||
		currentTime.Equal(eventConfirmationCutoff) {
		status = HoldStatusExpired
	}

	err = tx.QueryRow(ctx,
		"UPDATE holds SET status = $1 WHERE id = $2 AND event_id = $3 RETURNING id, status, quantity, deadline",
		status, id, eventId).Scan(&hold.id, &hold.status, &hold.quantity, &hold.deadline)
	if err != nil {
		return Hold{}, err
	}

	if status == HoldStatusConfirmed {
		_, err = tx.Exec(ctx,
			"INSERT INTO operations (id, status, quantity, deadline, event_id, hold_id) VALUES ($1, $2, $3, $4, $5, $6)",
			operationKey, HoldStatusConfirmed, hold.quantity, hold.deadline, eventId, hold.id)
		if err != nil {
			return Hold{}, err
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return Hold{}, err
	}

	if status == HoldStatusExpired {
		return Hold{}, ErrAfterConfirmationDeadline
	} else {
		return hold, nil
	}
}
