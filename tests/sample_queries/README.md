# Sample queries

Ten queries that run without errors but return the wrong rows, each paired with a corrected
version. They run against the `QryvantaSample` fixture database
(`tests/fixtures/mssql`, loaded with `docker compose run --rm fixtures`).

They serve three purposes:

1. **Demo:** open them in Qryvanta and see why the result is wrong.
2. **Regression data:** `tests/integration` runs every query and checks the results listed in
   [`manifest.json`](manifest.json), so the fixture data can't drift without notice.
3. **Acceptance tests for diagnostics (Phase 5):** each buggy query is the case a diagnostic
   rule must flag. Each corrected query is a case it must not flag.

| # | Scenario | Question | Buggy result | Correct result | Why |
|---|---|---|---|---|---|
| 01 | Employees without departments | Every employee with their department | 9 rows | 12 rows | `INNER JOIN` drops 2 employees with a NULL department and 1 with a deleted department |
| 02 | One-to-many fan-out | Total paid and items per customer | Nimal: 105.00 paid, 9 items (3 customers) | Nimal: 60.00 paid, 6 items (4 customers) | Items × payments multiply rows; inner join drops an unpaid customer |
| 03 | Duplicate records | Number of distinct customers | 8 | 6 | Same person entered with different case and spacing |
| 04 | `NOT IN` with a NULL | Customers who never ordered | 0 rows | 4 rows | A guest order has a NULL `CustomerID` |
| 05 | Date boundary | Orders placed in January | 3 rows | 4 rows | `BETWEEN … '2026-01-31'` stops at midnight |
| 06 | AND/OR precedence | Shipped or delivered orders to LK | 5 rows | 3 rows | `AND` binds tighter than `OR` |
| 07 | COUNT vs COUNT DISTINCT | Orders per customer | Nimal: 3 | Nimal: 2 | `COUNT` counts order lines after the join |
| 08 | Incorrect join key | Lines of order 103 with product names | 2 rows, wrong names | 3 rows | `ProductID` compared with `OrderItemID` |
| 09 | Aggregation granularity | Average order value | 50.83 | 76.25 | Averaged over lines (610 / 12), not orders (610 / 8) |
| 10 | Missing join predicate | Customer for each January order | 32 rows | 4 rows | Comma join without a predicate: 4 orders × 8 customers |

## Adding a scenario

1. Add the rows the scenario needs to `tests/fixtures/mssql/02_data.sql`, with a comment explaining them.
   Check that existing scenarios still hold.
2. Add `NN_name.buggy.sql` and `NN_name.correct.sql`. Start each with a comment stating the
   question and the bug. Use `ORDER BY` so row checks are deterministic.
3. Add the entry to `manifest.json`: row counts plus a few cell checks that show the difference.
4. Run the integration tests (see [docs/development/setup.md](../../docs/development/setup.md)).

All names and values are synthetic.
