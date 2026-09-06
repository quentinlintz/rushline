# Rushline

A headless inventory reservation and allocation service for high-demand ticket sales.

## Problem

Ticket drops create a rush of traffic. Systems handling this need to be...

1. **Safety**: to prevent overselling in a finite ticket inventory.
2. **Retries**: retrying a request cannot apply the same inventory operation twice. Achieved through idempotency.
3. **Durability**: to recover from a crash by restoring the last committed inventory state.
4. **Availability**: to process requests for sales while allowing for rejected requests if it risks overselling.

(In order of priority)

## Integration

A ticketing platform or venue backend delegates management of an inventory pool to Rushline through holds, confirmations, cancellations, and expirations. Rushline guarantees that confirmed allocations and active holds never exceed the pool's capacity.

## External

Rushline is not handling...

- Storefront client
- Payment processing
- Customer identity
