BEGIN;

CREATE TABLE events (
    id text PRIMARY KEY CHECK (id <> ''),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    capacity int NOT NULL CHECK (capacity > 0),
    onsale_opening timestamptz NOT NULL,
    hold_creation_cutoff timestamptz NOT NULL,
    confirmation_cutoff timestamptz NOT NULL,
    CHECK (onsale_opening > created_at),
    CHECK (hold_creation_cutoff > onsale_opening),
    CHECK (confirmation_cutoff > hold_creation_cutoff)
);

CREATE TABLE holds (
    id text CHECK (id <> ''),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    status int NOT NULL CHECK (status > 0 AND status < 5),
    quantity int NOT NULL CHECK (quantity > 0),
    deadline timestamptz NOT NULL CHECK (deadline > created_at),
    event_id text NOT NULL REFERENCES events (id),
    PRIMARY KEY (id, event_id)
);

CREATE TABLE operations (
    id text CHECK (id <> ''),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    status int NOT NULL CHECK (status > 0 AND status < 5),
    quantity int NOT NULL CHECK (quantity > 0),
    deadline timestamptz NOT NULL,
    event_id text NOT NULL,
    hold_id text NOT NULL,
    PRIMARY KEY (id, event_id),
    FOREIGN KEY (hold_id, event_id) REFERENCES holds (id, event_id)
);

CREATE INDEX idx_holds_deadline ON holds (status, deadline);

COMMIT;
