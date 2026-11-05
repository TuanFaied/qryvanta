-- Fix: an explicit JOIN with the join predicate.
SELECT o.OrderID, c.Name AS Customer
FROM dbo.Orders AS o
JOIN dbo.Customers AS c ON c.CustomerID = o.CustomerID
WHERE o.OrderDate >= '2026-01-01' AND o.OrderDate < '2026-02-01'
ORDER BY o.OrderID;
