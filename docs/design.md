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
