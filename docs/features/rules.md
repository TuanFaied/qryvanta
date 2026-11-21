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

## Rules

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
