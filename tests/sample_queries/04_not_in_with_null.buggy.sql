-- Question: which customers have never placed an order?
-- Bug: Orders contains a NULL CustomerID (a guest checkout). x NOT IN (..., NULL) is never TRUE,
-- so the query returns no rows at all.
SELECT c.CustomerID, c.Name
FROM dbo.Customers AS c
WHERE c.CustomerID NOT IN (SELECT o.CustomerID FROM dbo.Orders AS o)
ORDER BY c.CustomerID;
