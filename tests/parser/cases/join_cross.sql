-- CROSS JOIN has no ON predicate
SELECT d.n, m.n FROM dbo.Digits AS d CROSS JOIN dbo.Months AS m;
