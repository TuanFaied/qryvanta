-- a correlated scalar subquery compared in WHERE (latest order per customer)
SELECT o.* FROM dbo.Orders o
WHERE o.OrderDate = (SELECT MAX(o2.OrderDate) FROM dbo.Orders o2 WHERE o2.CustomerID = o.CustomerID);
