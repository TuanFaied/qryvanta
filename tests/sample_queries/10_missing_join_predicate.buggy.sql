-- Question: the customer for each order placed in January 2026.
-- Bug: the old-style comma join has no join predicate, so every order is paired with every
-- customer (a cartesian product).
SELECT o.OrderID, c.Name AS Customer
FROM dbo.Orders AS o, dbo.Customers AS c
WHERE o.OrderDate >= '2026-01-01' AND o.OrderDate < '2026-02-01'
ORDER BY o.OrderID, c.CustomerID;
