-- Fix: NOT EXISTS is not affected by NULLs in the subquery.
SELECT c.CustomerID, c.Name
FROM dbo.Customers AS c
WHERE NOT EXISTS (SELECT 1 FROM dbo.Orders AS o WHERE o.CustomerID = c.CustomerID)
ORDER BY c.CustomerID;
