-- a correlated EXISTS and NOT EXISTS
SELECT c.CustomerID
FROM dbo.Customers c
WHERE EXISTS (SELECT 1 FROM dbo.Orders o WHERE o.CustomerID = c.CustomerID)
  AND NOT EXISTS (SELECT 1 FROM dbo.Refunds r WHERE r.CustomerID = c.CustomerID);
