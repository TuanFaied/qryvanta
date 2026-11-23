# Diagnostic rules

Qryvanta's analyzer (`internal/analyzer`) runs rules over the parsed query and reports
**findings** (architecture decision D3). This page lists every rule. A test fails when a built-in
rule has no section here.

## How findings work

Each finding has:

- a **rule ID**,
- a **severity**: Critical, High, Medium, Low or Informational,
- a **classification**:
  - *Confirmed issue*: the evidence proves the issue.
  - *Potential issue*: the query may be wrong, depending on data or intent.
  - *Optimization suggestion*: the query is correct but could be faster or clearer.
- the position in the SQL,
- an explanation,
- the **evidence** behind it,
- and sometimes a **suggested correction**.

There are four kinds of evidence, from cheapest to most expensive:

- **static**: from the query text alone
- **metadata**: from the catalog: keys, nullability, data types
- **plan**: from an estimated or actual execution plan, always labelled as estimated or actual
- **probe**: from a generated read-only query that you approved first (D4)

Every finding is validated before it is shown:

- A *Confirmed* finding without evidence is rejected.
- A finding whose position, evidence or correction is outside the analyzed SQL is rejected.
- A finding with evidence of a kind its rule does not declare is rejected.

A rejected finding, a rule that fails, and a rule that crashes are reported as a rule failure.
The other rules still run.

A suggested correction is only text. It is shown as a diff against the original and is never
applied or executed. Apply it yourself if it is what you meant.

Batches with syntax errors are not analyzed. Fix the syntax first.

## Rule IDs

IDs look like `QRY-<AREA>-<NNN>`, for example `QRY-NULL-001`. An ID never changes meaning and is
never reused, so reports and suppressions can rely on it. Areas:

| Area | Covers |
|---|---|
| `NULL` | NULL comparisons and NULL logic |
| `LOGIC` | Boolean logic: operator precedence |
| `JOIN` | Joins: missing predicates, outer joins, keys, types and foreign keys |
| `PRED` | Predicates that cannot be true or add nothing |
| `DATE` | Date and time filters |
| `COUNT` | Counting |

## Rules

The rules read the parsed query, so text in comments and string literals never triggers them.
`QRY-JOIN-003` to `QRY-JOIN-006` also need metadata (they run only when the tab is connected),
and three static rules use metadata when it is there (see
[Metadata](#metadata-confirmations-and-dismissals)). They apply wherever a query can appear: CTEs, derived
tables, subqueries, every branch of a set operation, `UPDATE … FROM` and `DELETE … FROM`.

| ID | Title | Severity | Classification |
|---|---|---|---|
| QRY-JOIN-001 | JOIN without a predicate (cartesian product) | High | Potential |
| QRY-JOIN-002 | Outer join cancelled by a WHERE condition | High | Confirmed |
| QRY-JOIN-003 | Join on non-key columns (fan-out risk) | Medium | Potential |
| QRY-JOIN-004 | Join columns of different types | Medium | Potential |
| QRY-JOIN-005 | INNER JOIN on a nullable column | Medium | Potential |
| QRY-JOIN-006 | Join does not follow the foreign key | High | Potential |
| QRY-LOGIC-001 | AND and OR mixed without parentheses | Medium | Potential |
| QRY-NULL-001 | Comparison with NULL using = or <> | High | Confirmed |
| QRY-NULL-002 | NOT IN with a subquery that can return NULL | High | Potential (Confirmed for a NULL in the list) |
| QRY-PRED-001 | Contradictory conditions | High | Confirmed |
| QRY-PRED-002 | Redundant condition | Low | Optimization |
| QRY-DATE-001 | Function on a date column in a filter | Low | Optimization |
| QRY-DATE-002 | BETWEEN with a date as the end of a datetime range | Medium | Potential |
| QRY-COUNT-001 | COUNT(column) skips NULLs | Informational | Potential |
| QRY-COUNT-002 | COUNT over a join may count rows more than once | Medium | Potential |

### QRY-NULL-001

**Comparison with NULL using = or <>** · High · Confirmed · static evidence

`x = NULL`, `x <> NULL` and `x != NULL` compare with the NULL literal. The result is UNKNOWN for
every row, never TRUE, so a WHERE, ON or HAVING condition on it matches no rows, and a
`CASE WHEN x = NULL` branch is never taken.

```sql
SELECT * FROM Sales.Customers WHERE Region = NULL;      -- no rows, ever
```

Suggested correction: `Region IS NULL` (or `IS NOT NULL` for `<>` and `!=`).

This assumes `ANSI_NULLS ON`, the default. `SET ANSI_NULLS OFF` is deprecated, and future SQL
Server versions will not support it.

### QRY-NULL-002

**NOT IN with a subquery that can return NULL** · High · Potential · static evidence

`x NOT IN (SELECT y …)` means `x <> y1 AND x <> y2 AND …`. If any `y` is NULL, that comparison
is UNKNOWN, so the predicate is never true and the query returns no rows. There is no error.
`NOT (x IN (SELECT y …))` behaves the same way.

```sql
SELECT c.CustomerID FROM dbo.Customers AS c
WHERE c.CustomerID NOT IN (SELECT o.CustomerID FROM dbo.Orders AS o);  -- no rows if any CustomerID is NULL
```

Suggested correction:
`NOT EXISTS (SELECT 1 FROM dbo.Orders AS o WHERE o.CustomerID = c.CustomerID)`. NULLs in the
subquery don't affect NOT EXISTS. Unlike NOT IN, it also keeps rows where `x` is NULL.

The rewrite is offered only when it is safe:
- The subquery selects one column, from a FROM, without GROUP BY, HAVING, TOP or OFFSET.
- `x` is a variable, a literal, or a column qualified with a name the subquery does not use.

How it's classified:
- **Potential:** whether the subquery can return NULL depends on the column and the data.
  Metadata (nullability) or a probe can confirm it; that evidence comes in later issues.
- **Silenced:** when the subquery selects a non-NULL literal, or its WHERE requires the selected
  column `IS NOT NULL`.
- **Confirmed:** `x NOT IN (1, NULL)`, a NULL literal in a list, is always a confirmed issue.

### QRY-LOGIC-001

**AND and OR mixed without parentheses** · Medium · Potential · static evidence

AND is evaluated before OR, so `a OR b AND c` means `a OR (b AND c)`. The finding's explanation
shows how SQL Server groups the condition.

```sql
SELECT OrderID FROM dbo.Orders
WHERE Status = 'Shipped' OR Status = 'Delivered' AND ShipCountry = 'LK';
-- SQL Server reads: Status = 'Shipped' OR (Status = 'Delivered' AND ShipCountry = 'LK')
```

How it's classified:
- **Potential:** the grouping may be the intended one. The rule has no correction, because only
  the author knows which grouping was meant.
- **Reported:** a chain of ORs is reported once, when any of its operands is an unparenthesized
  AND.
- **Silenced:** parentheses around either the AND or the OR group.

### QRY-JOIN-001

**JOIN without a predicate (cartesian product)** · High · Potential · static evidence

Every row of one side is paired with every row of the other. The rule reports two shapes:

- A comma join (`FROM a, b`) with no WHERE condition that refers to both sides.
- An INNER, LEFT, RIGHT or FULL JOIN whose ON does not refer to both sides (`ON a.x = a.y`,
  `ON 1 = 1`, `ON b.active = 1`).

```sql
SELECT o.OrderID, c.Name FROM dbo.Orders AS o, dbo.Customers AS c
WHERE o.OrderDate >= '2026-01-01';   -- every order paired with every customer
```

How it's classified:
- **Potential:** the product is certain, but whether it is a bug isn't, so add the join condition.
- **Not reported:** `CROSS JOIN` and `APPLY`, which say what they do.
- **Silenced:** a comma join with a derived table that has at most one row (an aggregate without
  GROUP BY, `TOP (1)`, or no FROM).
- **Silenced, can't be checked without metadata:** conditions with unqualified columns or with
  subqueries. Names from an outer query (in a correlated subquery) don't link the two sides.

### QRY-JOIN-002

**Outer join cancelled by a WHERE condition** · High · Confirmed · static evidence

Rows that a LEFT JOIN adds without a match have NULL in every column of the right table (for a
RIGHT JOIN, the left table). A WHERE condition on those columns that is never true for NULL
removes exactly those rows, so the outer join returns what an INNER JOIN would.

```sql
SELECT e.Name, d.Name FROM dbo.Employees AS e
LEFT JOIN dbo.Departments AS d ON d.DepartmentID = e.DepartmentID
WHERE d.Active = 1;   -- employees without a department are gone
```

Suggested correction: move the condition into ON:
`ON d.DepartmentID = e.DepartmentID AND d.Active = 1`. If an inner join is what you want, write
INNER JOIN.

How it's classified:
- **Confirmed:** the condition provably rejects NULL. It is a comparison, `IN`, `BETWEEN` or
  `LIKE` directly on a column of the optional side, an AND with such a condition, or an OR whose
  every branch is one.
- **Not reported:**
  - `IS NULL` (`OR d.Active IS NULL` keeps the unmatched rows, and `WHERE d.ID IS NULL` is the
    usual anti-join);
  - functions like `ISNULL(d.Active, 1) = 1`;
  - `NOT IN (subquery)`, which is true for NULL when the subquery is empty;
  - FULL JOIN.

### QRY-PRED-001

**Contradictory conditions** · High · Confirmed · static evidence

Two conditions in one AND chain on the same column that no value satisfies:

```sql
SELECT * FROM dbo.Orders WHERE Status = 1 AND Status = 2;          -- never true
SELECT * FROM dbo.Orders WHERE Total > 500 AND Total < 100;        -- never true
SELECT * FROM dbo.Orders WHERE ShippedAt IS NULL AND ShippedAt > 0; -- never true
```

How it's classified:
- **Confirmed:** no row can satisfy the AND. In a WHERE without OR, the query returns no rows.
- **What is analyzed:** only comparisons of a column with a number, written either way round
  (`=`, `<>`, `<`, `>`, `<=`, `>=`), and `IS [NOT] NULL`. Each pair of conditions on the same
  column, written the same way, is checked.
- **Skipped as not provable:**
  - strings, because collations decide whether `'a'` equals `'A'`;
  - dates, variables and expressions;
  - conditions under NOT;
  - contradictions that need three or more conditions together.

### QRY-PRED-002

**Redundant condition** · Low · Optimization · static evidence

A condition in an AND chain that another one already implies, or the same condition written
twice:

```sql
WHERE Total > 500 AND Total > 100     -- Total > 100 adds nothing
WHERE Status = 4 AND Status <> 2      -- Status <> 2 adds nothing
WHERE o.CustomerID = c.CustomerID AND o.CustomerID = c.CustomerID
```

How it's classified:
- **Optimization:** the query is correct, but the redundant condition may hide a typo (another
  value was meant). The finding points at the condition that adds nothing.
- **Not reported:** `IS NOT NULL` next to a comparison, which some people write for clarity.
  It is implied, but not reported.
- **What is analyzed:** numbers only, as in QRY-PRED-001. Exact duplicates are found for any
  condition.

### QRY-DATE-001

**Function on a date column in a filter** · Low · Optimization · static evidence

A filter that applies a date function or a conversion to a column, compared with a value that
does not depend on any column. SQL Server must compute the function for every row and cannot
seek an index on the column.

```sql
WHERE YEAR(OrderDate) = 2024
WHERE CONVERT(date, OrderDate) = '2024-01-05'
WHERE DATEADD(day, 30, OrderDate) > GETDATE()
```

Suggested corrections, each with the same result:

| Filter | Rewrite |
|---|---|
| `YEAR(c) = 2024` | `c >= '20240101' AND c < '20250101'` |
| `CAST(c AS date) = X` | `c >= X AND c < DATEADD(day, 1, X)` |
| `DATEADD(day, n, c) op X` | `c op DATEADD(day, -n, X)` |

- The `DATEADD` rewrite is offered only for units of fixed length: day, week, hour, minute and
  second. `c + 1 month` is not the same as `x - 1 month` at month ends.
- Other functions get the finding without a rewrite.

How it's classified:
- **Optimization:** the result is correct. A combination with other problems (for example a
  large scan in the actual plan) can raise it later.
- **Conversion to `date` is a special case:** SQL Server can still seek with it, but its row
  estimates are often poor. The finding says so.
- **Not reported:** functions on both sides (a join condition) and functions only on the value
  side (`d > DATEADD(day, -30, GETDATE())`).

### QRY-DATE-002

**BETWEEN with a date as the end of a datetime range** · Medium · Potential · static evidence

```sql
WHERE OrderDate BETWEEN '2026-01-01' AND '2026-01-31'   -- stops at 2026-01-31 00:00:00
```

When the column has a time of day, `'2026-01-31'` means midnight at the start of that day, so
the rest of 31 January is missing. An end of `'2026-01-31 23:59:59'` misses fractions of a
second.

Suggested correction: a half-open range,
`OrderDate >= '2026-01-01' AND OrderDate < '2026-02-01'`. It works whether the column is `date`
or `datetime`.

How it's classified:
- **Potential:** for a `date` column, BETWEEN is correct. Metadata evidence confirms it when the
  column is `datetime`, `datetime2` or `datetimeoffset` (the metadata rules issue).
- **Not reported:** an end with another time of day, which is explicit, and `NOT BETWEEN`.

### QRY-COUNT-001

**COUNT(column) skips NULLs** · Informational · Potential · static evidence

`COUNT(Email)` counts the rows where Email has a value, not the rows. When the column can be
NULL, the two differ.

Suggested correction: `COUNT(*)`, if the intent is to count rows.

How it's classified:
- **Potential, informational:** counting values is often what was meant. Nullability metadata
  will raise or silence it.
- **Silenced when the query already guarantees a value:**
  - a WHERE condition on the column that is never true for NULL (`IS NOT NULL`, a comparison,
    `IN`, `LIKE`, `BETWEEN`);
  - or an `=` on the column in an inner join's ON, when the FROM has no outer join.
- **Not reported:** `COUNT(DISTINCT …)`, `COUNT(*)` and `COUNT(1)`.

### QRY-COUNT-002

**COUNT over a join may count rows more than once** · Medium · Potential · static evidence

```sql
SELECT o.CustomerID, COUNT(o.OrderID) AS Orders, SUM(oi.Quantity) AS Units
FROM dbo.Orders AS o JOIN dbo.OrderItems AS oi ON oi.OrderID = o.OrderID
GROUP BY o.CustomerID;   -- Orders counts order lines
```

The query aggregates columns of two tables of a join, so the aggregates run once per joined
row. `COUNT(o.OrderID)` counts an order once per matching line.

Suggested correction: `COUNT(DISTINCT o.OrderID)`, if `o.OrderID` identifies an order.

How it's classified:
- **Potential:** the join may match at most one row. The fan-out probe (#70) can measure it.
- **Reported:** `COUNT(t.column)` without DISTINCT, when another aggregate of the same query uses
  a different table of the join.
- **Left to the fan-out probe:** `COUNT(*)` over a join, which says nothing statically (counting
  joined rows is often intended).

## Probes: measured evidence

With a connection, the analysis also proposes **probes**: read-only queries, generated from fixed
templates, that measure what a finding claims. Every probe is listed in the analysis panel with
its SQL and what it would prove, and nothing runs until you click **Run N probes**. Probes run
under the read-only guard with the probe timeout, two at a time, and **Cancel** stops them.

| Rule | Probe | Result |
|---|---|---|
| QRY-JOIN-003 | Rows per value of the join columns | Values that repeat → *Confirmed* (for example "up to 4 rows per value, 1.75 on average"); none → dismissed |
| QRY-JOIN-005 | Rows without a match on the join columns | Rows dropped → *Confirmed* ("3 of 12 rows, 2 with NULL"); none → dismissed |
| QRY-JOIN-006 | Rows without a match on the join as written | Rows dropped → *Confirmed*; none → dismissed |
| QRY-NULL-002 | NULLs in the subquery's column | NULLs → *Confirmed*; none → dismissed (the column still allows NULL) |
| QRY-DATE-002 | Rows after the BETWEEN end and before the next day | Rows missed → *Confirmed*; none → dismissed |

A dismissed finding is listed under the probes with the measurement that dismissed it. A
measurement describes today's data: a key, a NOT NULL constraint or a corrected query guarantees
it for good. The probe budget is in Settings ("Maximum probe queries per run", "Probe query
timeout").

## Metadata: confirmations and dismissals

When the analyzed tab is connected, Qryvanta reads the catalog entries of the tables and views
the query names: column types, nullability, keys (primary key, unique constraints, unique indexes
without a filter) and foreign keys. These are read-only catalog queries, cached per connection
and cleared by the explorer's Refresh. Tables it cannot read (missing, no permission, linked
servers, no database selected) are listed under the findings, and never fail the analysis.

| Rule | Metadata |
|---|---|
| QRY-DATE-002 | Confirmed when the column is `datetime`, `datetime2`, `smalldatetime` or `datetimeoffset`; dismissed when it is `date` |
| QRY-NULL-002 | Dismissed when the subquery's column is NOT NULL; otherwise the nullable column is named |
| QRY-COUNT-001 | Dismissed when the column is NOT NULL (unless on the optional side of an outer join); otherwise the nullable column is named |

### QRY-JOIN-003

**Join on non-key columns (fan-out risk)** · Medium · Potential · metadata evidence

Two shapes, judged from the keys of the joined tables:

- **Many-to-many:** neither side's join columns cover a primary key or unique set, so a row on
  either side can match several on the other.

  ```sql
  SELECT … FROM dbo.Orders AS o JOIN dbo.Payments AS p ON p.OrderID = o.CustomerID
  ```

- **Two one-to-many joins from one table:** each row of the table is repeated per match on both
  sides, so the two tables' rows multiply, and SUM or COUNT over either is inflated.

  ```sql
  FROM dbo.Orders AS o
  JOIN dbo.OrderItems AS oi ON oi.OrderID = o.OrderID   -- one-to-many
  JOIN dbo.Payments AS p ON p.OrderID = o.OrderID       -- one-to-many: items × payments
  ```

How it's classified:
- **Potential:** whether rows actually multiply depends on the data. The fan-out probe (#70)
  measures it.
- **How keys are judged:** conditions on a constant count as covered (`AND l.LineNumber = 1`
  completes a key of (OrderID, LineNumber)).
- **Not judged:** joins to views (they have no keys to judge), unresolved tables, and joins whose
  conditions on one side span several tables.

### QRY-JOIN-004

**Join columns of different types** · Medium · Potential · metadata evidence

A join that compares columns of different type families: a string with a number or date,
`varchar` with `nvarchar`, or `date` with a datetime type.

```sql
FROM dbo.Legacy AS g JOIN dbo.Codes AS c ON c.Code = g.Code   -- varchar(10) = nvarchar(10)
```

SQL Server converts one side for every row, by data type precedence:
- a conversion can change what matches (`'01' = 1`) or fail for values that don't convert;
- it prevents an index seek on the converted column;
- a `date` compared with a datetime matches only at midnight.

How it's classified:
- **Potential:** the implicit-conversion warning in an actual plan (Phase 6) confirms it.
- **Not reported:** numbers of different sizes (`int` and `bigint`), which compare correctly.
  Different collations are an error that SQL Server reports itself.

### QRY-JOIN-005

**INNER JOIN on a nullable column** · Medium · Potential · metadata evidence

```sql
SELECT e.Name, d.Name FROM dbo.Employees AS e
JOIN dbo.Departments AS d ON d.DepartmentID = e.DepartmentID   -- Employees.DepartmentID allows NULL
```

NULL equals nothing, so employees without a department have no match and the INNER JOIN drops
them without a sign. If they belong in the result, use a LEFT JOIN.

How it's classified:
- **Potential:** dropping them may be intended. The finding names the column.
- **Silenced:** when WHERE already requires a value in the column, or the column's table is on
  the optional side of an outer join.

### QRY-JOIN-006

**Join does not follow the foreign key** · High · Potential · metadata evidence

Two tables linked by a single-column foreign key, joined with the key on one side and another
column on the other:

```sql
FROM dbo.OrderItems AS oi
JOIN dbo.Products AS p ON p.ProductID = oi.OrderItemID   -- OrderItems.ProductID references Products.ProductID
```

When the values happen to be in the same range, the join returns plausible, wrong rows and
drops the rest, without an error.

Suggested correction: join on the foreign key column (`p.ProductID = oi.ProductID`). It is
offered only when the wrong column is qualified; an unqualified name could become ambiguous.

How it's classified:
- **Potential:** joining on another column can be intended.
- **Not reported:** a join that follows any foreign key between the two tables, and self-joins.
