-- a derived table with a column alias list, grouped inside
SELECT t.CustomerID, t.Total
FROM (SELECT CustomerID, SUM(Amount) FROM dbo.Payments GROUP BY CustomerID) AS t (CustomerID, Total)
WHERE t.Total > 100;
