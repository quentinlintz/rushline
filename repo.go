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

func (r Repository) GetHoldById(ctx context.Context, id string, eventId string) (Hold, error) {
	var hold Hold
	query := "SELECT id, status, quantity, deadline, event_id FROM holds WHERE id = $1 AND event_id = $2"
	err := r.conn.QueryRow(ctx, query, id, eventId).
		Scan(&hold.id, &hold.status, &hold.quantity, &hold.deadline, nil)
	if err != nil {
		return Hold{}, err
	}
	return hold, nil
}
