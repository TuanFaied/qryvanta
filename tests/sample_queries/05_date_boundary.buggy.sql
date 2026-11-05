-- Question: orders placed in January 2026.
-- Bug: OrderDate has a time of day. BETWEEN ... AND '2026-01-31' stops at 2026-01-31 00:00:00,
-- so the order placed on 31 January at 16:45 is missing.
SELECT OrderID, OrderDate
FROM dbo.Orders
WHERE OrderDate BETWEEN '2026-01-01' AND '2026-01-31'
ORDER BY OrderID;
