# Design

## Holds

A hold represents reserved inventory (removed from availability). It may be confirmed, cancelled, or expired. Confirmation converts the held quantity into a confirmed allocation. Cancellation and expiration release held quantities. Requests are rejected when insufficient inventory exists.

## Timing

Event time is separate from the onsale window. Onsale opening allows for new holds and new holds are cut off after a certain point separate from the event start time. The hold will expire either at the confirmation cutoff or its own hold expiration cutoff, whichever comes first. Each hold has a configurable TTL.

## State

### Authoritative

- Inventory capacity
- Hold quantity, status, expiration deadline
- Confirmed allocations
- Retry/command outcomes

### Derived values

- Available quantity
- Sold-out status
- Historical sales totals/reports

## Workloads

### Operational

- Create and config inventory
- Create, confirm, cancel, expire holds
- Check command, hold, and inventory status
- Check current available or sold-out inventory

### Analytical

- Sales totals and rate over time
- Hold-to-confirmation conversion
- Abandoned or expired-hold rates

## Failure

- Recovery restores last committed inventory state
- Crashes do not prove an in-flight operation failed
- A commit may succeed before the response is delivered
- Retrying the same command shouldn't duplicate its effect!

## Invariants

- No overselling: active holds + confirmed allocation can never exceed capacity
  - Counterexample: Inventory is 10, two requests place hold for 6 each and succeed. Rushline now claims 12 inventory units held.

- Confirmation converts a hold: during confirmation, active held quantity decreases by the same amount that confirmed quantity increases with the availability remaining unchanged
  - Counterexample: Original quantity of 2 is simultaneously counted as 2 held and 2 confirmed.

- Expired hold cannot be confirmed
  - Counterexample: Hold expires at 1:00, confirmation arrives at 1:01, Rushline confirms it and allocates inventory.

- Duplicate requests change state at most once. If a request reuses an idempotency key with the same payload: legitimate retry. Reusing the key with a different payload: rejected.
  - Counterexample: A confirmation converts the hold, the response is lost, the client retries the identical confirmation with the same key, Rushline executes it again and applies confirmation twice.

## Transition table

| Command | Current hold state | Guards | Next state | Inventory effect | Result |
| -------- | ------- | ------- | ------- | ------- | ------- |
| Create | Nonexistent | Window open; requested quantity is a positive integer; available quantity >= requested quantity | Active | Held += quantity | Hold created |
| Create | Nonexistent | Guard failure | Nonexistent | None | Reject |
| Confirm | Active | now < effective deadline | Confirmed | Held -= quantity, confirmed += quantity | Allocation confirmed |
| Confirm | Active | now >= effective deadline | Expired | Held -= quantity | Rejected as expired |
| Confirm | Confirmed | - | Confirmed | None | Already confirmed |
| Confirm | Cancelled | - | Cancelled | None | Rejected: cancelled |
| Confirm | Expired | - | Expired | None | Rejected: expired |
| Confirm | Nonexistent | - | Nonexistent | None | Not found |
| Cancel | Active | now < effective deadline | Cancelled | Held -= quantity | Hold cancelled |
| Cancel | Active | now >= effective deadline | Expired | Held -= quantity | Rejected: expired |
| Cancel | Confirmed | - | Confirmed | None | Rejected: already confirmed |
| Cancel | Cancelled | - | Cancelled | None | Already cancelled |
| Cancel | Expired | - | Expired | None | Rejected: expired |
| Cancel | Nonexistent | - | Nonexistent | None | Not found |
| Expire | Active | now >= effective deadline | Expired | Held -= quantity | Hold expired |
| Expire | Active | now < effective deadline | Active | None | Not yet eligible |
| Expire | Confirmed | - | Confirmed | None | Not eligible |
| Expire | Cancelled | - | Cancelled | None | Not eligible |
| Expire | Expired | - | Expired | None | Already expired |
| Expire | Nonexistent | - | Nonexistent | None | Not found |

- The guard check, state transition, inventory update, and command outcome recording occur atomically.
- Before evaluating the table, Rushline checks the idempotency key. The same key and payload returns the stored outcome without reevaluating the transition. The same key with a different payload is rejected.
- Effective deadline = min(hold expiration, confirmation cutoff).
- A background expiration process expires overdue holds. Every operation that depends on an active hold also validates its effective deadline.

## Data Models

### Event

We can derive availability if we keep `capacity` set when the Event is first created because the Hold table's quantities can be summed, so we should not store availability as a field. Holds that consume availability are active or confirmed.

- **id**: unique among events, string
- **created_at**: timestamptz, server-owned metadata
- **updated_at**: timestamptz, last persisted config change, initially equal to created_at
- **capacity**: positive integer
- **onsaleOpening**: timestamptz, must be in the future, checked on creation
- **holdCreationCutoff**: timestamptz, must be after onsaleOpening, checked on creation
- **confirmationCutoff**: timestamptz, must be after holdCreationCutoff, checked on creation

### Hold

An index will hold active holds ordered by deadline, so we can quickly scan which holds should be expired. Currently, `event.go` contains a slice of strings, `expirableHolds`, that is maintained for scanning. Another useful index could be status and quantity so consumed availability can be quickly calculated.

- **id**: unique within an event, string
- **created_at**: timestamptz, server-owned metadata
- **updated_at**: timestamptz, server-owned metadata
- **status**: enum, 1-4 (active, confirmed, cancelled, expired)
- **quantity**: positive integer
- **deadline**: timestamptz, must be after creation of the hold
- **event_id**: foreign key to *one* Event

### Operation

Outcome of an operation that references a Hold snapshot. Operations are results of successful hold transition outcomes so the client can replay by sending a request for the *same* hold id and operation key in the header.

- **id**: unique within an event, string (operation key from the header of the request)
- **created_at**: timestamptz, server-owned metadata
- **status**: enum, 1-4 (active, confirmed, cancelled, expired)
- **quantity**: positive, integer
- **deadline**: timestamptz, must be after creation of the hold
- **event_id**: foreign key to *one* Event
- **hold_id**: foreign key to the *one* Hold that belongs to the Event in event_id

## Confirming

Past operations are searched for an event-scoped operation key. If the key and requested hold id match, the recorded snapshot is returned to the client, even after the deadline. This allows confirmations to be replayed when a response has been lost. Reusing the same event/key with a different hold is rejected with an "operation key conflict".

Flow for confirming a hold:

1. Start the transaction with Read Committed isolation.
2. If the event exists, establish event-row lock for concurrency protection. This is so a second request waits, then checks for a committed outcome (release this lock when the transaction ends).
3. Read operations for the event-scoped key. If an operation is found, compare its hold id with the requested hold id. A match selects the recorded snapshot for replay; a different hold id is rejected as a key conflict. Neither case transitions the hold again.
4. If no operation is found, read the event and hold. Reject if either is missing, and apply the status and deadline guards from the transition table.
5. For an eligible hold, update its status from "active" to "confirmed"
6. Record the successful operation outcome.
7. Commit before returning the successful outcome to the client. Replay and rejection paths must also finish their transaction and release any locks before responding.

If a database failure aborts the transaction, its changes are rolled back. For example, if the operation row cannot be inserted, the hold status mutation must also be rolled back. The overdue-hold rejection follows the transition table: the hold becomes expired and releases inventory, without recording a successful confirmation outcome.

Concurrent requests for the same hold with matching operation keys must be coordinated before making their decisions. If the first request commits, the second waits until the commit occurs, reads, and then replays its recorded outcome. If rolled back, the second request will be evaluated normally. A later request for the same confirmed hold with a different operation key is rejected as "hold already confirmed".

How the concurrency works:

1. Identical requests A and B are received simultaneously.
2. Request A acquires the lock while B waits to read anything until the transaction releases it.
3. If A succeeds in confirming and commits, the lock is released and B reads operations for the event-scoped key, finds it, and responds with the same outcome as A. B can replay the outcome even if A's response is lost
4. If A's transaction aborts, the status is rolled back (no changes persist from that transaction) and B begins the process normally, after acquiring the lock it reads operations for the event-scoped key.
