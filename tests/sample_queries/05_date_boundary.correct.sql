-- Fix: use a half-open range: from the first instant of the month up to, not including, the next.
SELECT OrderID, OrderDate
FROM dbo.Orders
WHERE OrderDate >= '2026-01-01' AND OrderDate < '2026-02-01'
ORDER BY OrderID;
