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

---

# Map-scan

The simplest approach is scanning over the existing `event.holds` in its entirety to determine which holds to expire.

## Map-scan method

System specs:
> go version go1.27.0 darwin/arm64
> cpu: Apple M4

Timing includes the `scanForExpiredHolds` function call and computing the supplied time. Setup, verification, and restoring the ten expired holds are outside the timing.

Tested commit: [7011e87](https://github.com/quentinlintz/rushline/commit/7011e87ff63a990fbc4c3468a9ff1de2c6ccb117)
Command ran:

```
go test -bench=BenchmarkScanForExpireHolds
```

## Map-scan results

_These results measure isolated sweep time, not the mutex wait time._

These 3 runs were automatically calibrated, so iteration count is also included.

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

---

# Indexed

A new slice, `event.expirableHolds` was created to maintain a list of active holds ids ordered by their due date. The scan operates on this rather than the entire `event.holds` map. Sorting by deadline also lets us stop when we encounter a hold that isn't due yet, at the time of the scan. Hold creation pushes the hold's id into `event.expirableHolds` and transitioning it to confirmed, cancelled, or expired removes it. Overdue, active hold ids can exist in this new slice; they are removed when `scanForExpiredHolds(currentTime)` is called.

## Indexed method

System specs:
> go version go1.27.0 darwin/arm64
> cpu: Apple M4

Timing includes the `scanForExpiredHolds` function call and computing the supplied time. Setup, verification, and restoring the ten expired holds are outside the timing.

Tested commit: [79ee244](https://github.com/quentinlintz/rushline/commit/79ee24454db569d77f86279d3fc5d8969ef84d87)
Command ran:

```
go test -bench=BenchmarkScanForExpireHolds -benchtime=1000x -count=3
```

## Indexed results

I'm taking the median of 3 repetitions per hold count. Running automatically calibrated was too demanding. Holds are created during initial setup; each iteration verifies all holds and restores 10 holds and their index entries. Faster timed scans means automatic calibration requests more iterations, multiplying that untimed work.

| Hold count | Repetition 1 | Repetition 2 | Repetition 3 |
| --- | --- | --- | --- |
| 1,000 | 795.5 | 519.7 | 530.2 |
| 10,000 | 559.4 | 521.2 | 544.8 |
| 100,000 | 646.8 | 962.8 | 666.8 |

*Measurements are in ns/op:

---

## Conclusion

Total holds grew 100x while my median indexed scan time grew by about 1.26x. However, if every hold were due, we could not return early at any point, so there wouldn't be a flat curve like we saw with the indexed results. Timing would scale with the number of _expirable_ holds, O(k), where 'k' is active holds due at the supplied time.

| Total holds | Original ns/op | Indexed ns/op | ~Speedup |
| --- | --- | --- | --- |
| 1,000 | 10,383 | 530.2 | 19.6x |
| 10,000 | 76,876 | 544.8 | 141x |
| 100,000 | 721,864 | 666.8 | 1,083x |

- Median ns/op!
- The runs are ran slightly differently, so they're not a controlled, statistical comparison
- Write-maintenance costs and concurrent lock waiting were not measured.
