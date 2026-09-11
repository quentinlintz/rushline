# Expiration Scan

`scanForExpiredHolds` in `event.go` sweeps for active, past-due holds and transitions them to expired.

## Question

Active holds have deadlines that could expire and release quantity back to the event's inventory. To check overdue holds, the structure is locked with a mutex to prevent any other operations like creating or confirming holds. How can I measure and optimize this process?

## Workload

I created 3-tiers of events:

- 1,000 holds
- 10,000 holds
- 100,000 holds

Each tier _always_ has 10 active holds exactly at their deadline. The remaining holds are also active, with deadlines _after_ the time supplied to the scan.

## Method

System specs:
> go version go1.27.0 darwin/arm64
> cpu: Apple M4

Timing includes the `scanForExpiredHolds` function call and computing the supplied time. Setup, verification, and restoring the ten expired holds are outside the timing.

Tested commit: [7011e87](https://github.com/quentinlintz/rushline/commit/7011e87ff63a990fbc4c3468a9ff1de2c6ccb117)
Command ran:

```
go test -bench=BenchmarkScanForExpireHolds
```

## Results

_These results measure isolated sweep time, not the mutex wait time._

### Run 1

| Hold count | Iterations | ns/op |
| --- | --- | --- |
| 1,000 | 113,350 | 10,383 |
| 10,000 | 14,924 | 76,218 |
| 100,000 | 1,592 | 721,864 |

### Run 2

| Hold count | Iterations | ns/op |
| --- | --- | --- |
| 1,000 | 111,728 | 10,846 |
| 10,000 | 15,612 | 76,876 |
| 100,000 | 1,665 | 728,511 |

### Run 3

| Hold count | Iterations | ns/op |
| --- | --- | --- |
| 1,000 | 117,445 | 10,235 |
| 10,000 | 15,391 | 78,337 |
| 100,000 | 1,753 | 654,973 |

## Interpretation

I expected linear scaling, since the holds map needs traversed for each function call. They are also unordered, so we can't guarantee at any point that all overdue holds are expired unless we cover each one.

This matches my prediction. 1,000 to 10,000 and 10,000 to 100,000 increases ns/op 7.1-7.7x for the first and 8.4-9.5x for the second.

## Next hypothesis

Possible index: `deadline`: to quickly find overdue holds AND `status`: to find active (eligible) holds to expire

This index may make searching for _active holds at or after the deadline_ quicker. But the index must also be maintained per write, so creating and updating holds could take longer.
