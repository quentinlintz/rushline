package rushline

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
)

type Repository struct {
	conn *pgx.Conn
}

func NewRepository(ctx context.Context, connStr string) (*Repository, error) {
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		return nil, err
	}
	return &Repository{
		conn: conn,
	}, nil
}

func (r Repository) CreateEvent(ctx context.Context, event *Event) (*Event, error) {
	err := r.conn.QueryRow(ctx,
		"INSERT INTO events (id, capacity, onsale_opening, hold_creation_cutoff, confirmation_cutoff) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		event.id, event.capacity, event.onsaleOpening, event.holdCreationCutoff, event.confirmationCutoff).Scan(&event.id)
	return event, err
}

func (r Repository) CreateHold(ctx context.Context, hold Hold, eventId string) (Hold, error) {
	err := r.conn.QueryRow(ctx,
		"INSERT INTO holds (id, status, quantity, deadline, event_id) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		hold.id, hold.status, hold.quantity, hold.deadline, eventId).Scan(&hold.id)
	return hold, err
}

func (r Repository) CreateOperation(ctx context.Context, id string, hold Hold, eventId string) (Hold, error) {
	err := r.conn.QueryRow(ctx,
		"INSERT INTO operations (id, status, quantity, deadline, event_id, hold_id) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id",
		id, hold.status, hold.quantity, hold.deadline, eventId, hold.id).Scan(&id)
	return hold, err
}

func (r Repository) ReadHold(ctx context.Context, id string, eventId string) (Hold, error) {
	var hold Hold
	err := r.conn.QueryRow(ctx,
		"SELECT id, status, quantity, deadline, event_id FROM holds WHERE id = $1 AND event_id = $2",
		id, eventId).Scan(&hold.id, &hold.status, &hold.quantity, &hold.deadline, nil)
	if err != nil {
		return Hold{}, err
	}
	return hold, nil
}

func (r Repository) ReadOperation(ctx context.Context, id string, holdId string, eventId string) (Hold, error) {
	var hold Hold
	err := r.conn.QueryRow(ctx,
		"SELECT hold_id, status, quantity, deadline, event_id FROM operations WHERE id = $1 AND hold_id = $2 AND event_id = $3",
		id, holdId, eventId).Scan(&hold.id, &hold.status, &hold.quantity, &hold.deadline, nil)
	if err != nil {
		return Hold{}, err
	}
	return hold, nil
}

func (r Repository) ConfirmHold(ctx context.Context, id string, operationKey string, eventId string) (Hold, error) {
	var hold Hold
	tx, err := r.conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Hold{}, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		"SELECT 1 FROM events WHERE id = $1 FOR UPDATE",
		eventId)
	if err != nil {
		return Hold{}, err
	}

	err = tx.QueryRow(ctx,
		"SELECT hold_id, status, quantity, deadline FROM operations WHERE id = $1 AND hold_id = $2 AND event_id = $3",
		operationKey, id, eventId).Scan(&hold.id, &hold.status, &hold.quantity, &hold.deadline)
	if err == nil {
		return hold, nil
	} else if err != pgx.ErrNoRows {
		return Hold{}, err
	}

	err = tx.QueryRow(ctx,
		"SELECT id, status, quantity, deadline, event_id FROM holds WHERE id = $1 AND event_id = $2",
		id, eventId).Scan(&hold.id, &hold.status, &hold.quantity, &hold.deadline, nil)
	if err != nil {
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

	err = tx.QueryRow(ctx,
		"UPDATE holds SET status = $1 WHERE id = $2 AND event_id = $3 RETURNING id, status, quantity, deadline",
		HoldStatusConfirmed, id, eventId).Scan(&hold.id, &hold.status, &hold.quantity, &hold.deadline)
	if err != nil {
		return Hold{}, err
	}

	_, err = tx.Exec(ctx,
		"INSERT INTO operations (id, status, quantity, deadline, event_id, hold_id) VALUES ($1, $2, $3, $4, $5, $6)",
		operationKey, HoldStatusConfirmed, hold.quantity, hold.deadline, eventId, hold.id)
	if err != nil {
		return Hold{}, err
	}

	if err = tx.Commit(ctx); err != nil {
		return Hold{}, err
	}

	return hold, nil
}
