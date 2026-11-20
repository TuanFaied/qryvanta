-- a join on two columns: the ON predicate is an AND of two comparisons
SELECT *
FROM dbo.OrderLines l
JOIN dbo.Prices p ON p.ProductID = l.ProductID AND p.ValidFrom <= l.OrderDate;
