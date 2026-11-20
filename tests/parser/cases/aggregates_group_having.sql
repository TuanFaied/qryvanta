-- aggregates with GROUP BY and HAVING, including COUNT(*), COUNT(DISTINCT) and STRING_AGG
SELECT CustomerID, COUNT(*) AS Lines, COUNT(DISTINCT OrderID) AS Orders,
       STRING_AGG(Status, ',') AS Statuses
FROM dbo.Orders
GROUP BY CustomerID
HAVING COUNT(*) > 1 AND SUM(Amount) >= 100;
