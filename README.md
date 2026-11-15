# Rushline

A headless inventory reservation and allocation service for high-demand ticket sales. This project was an experiment as I read [Designing Data-Intensive Applications](https://www.goodreads.com/review/show/8889919757) between August-November 2026.

## Problem

Ticket drops create a rush of traffic. Systems handling this need to be...

1. **Safety**: to prevent overselling in a finite ticket inventory.
2. **Retries**: retrying a request cannot apply the same inventory operation twice. Achieved through idempotency.
3. **Durability**: to recover from a crash by restoring the last committed inventory state.
4. **Availability**: to process requests for sales while allowing for rejected requests if it risks overselling.

(In order of priority)

### Scope

Since this was meant to be an experiment, there is no runnable server entry point. All interactions are observable through the tests.

Two storage patterns are implemented:

1. In-memory: `./event.go` and `./hold.go` contain structs with parameters and functions containing operations against those structs.
2. Postgres: `./repo.go` contains a connection to an actual db with `./migrations/000001_create_initial_schema.sql` containing the schema and constraints. Confirmation/replay is implemented as the domain command.

This way, I could exercise row-locks and contention, but keep a reference implementation alongside it. (See `./server.go` for the distinction between these two)

The API contract is defined in `./openapi.yml` for reference only. There is no OpenAPI conformance checking.

## Integration

A ticketing platform or venue backend delegates management of an inventory pool to Rushline through holds, confirmations, cancellations, and expirations. Rushline offers a confirmation/replay command on those holds, transitioning them from active to confirmed.

## External

Rushline is not handling...

- Storefront client
- Payment processing
- Customer identity

## Setup

Start your Docker daemon.

Clone this repo and download dependencies. Go 1.27.0 is required.

```bash
git clone https://github.com/quentinlintz/rushline
cd rushline
go mod download
```

### Tests

I introduced [testify](https://github.com/stretchr/testify) late into this project. So, some tests are inconsistently written.

Rushline also uses [testcontainers](https://testcontainers.com/) to rapidly spin up ephemeral pg containers. See my [LinkedIn post](https://www.linkedin.com/feed/update/urn:li:activity:7523022850362867713/) about the benefits of these. Tests create postgres containers and apply the migration themselves.

You can run tests with

```bash
go test ./...
```

Appending `-race` will check data races and `-count=2` repeats tests.

```bash
go test -race -count=2 ./...
```
