-- CROSS APPLY and OUTER APPLY with a correlated derived table and a table-valued function
SELECT c.Name, last.OrderID, s.value
FROM dbo.Customers c
CROSS APPLY (SELECT TOP (1) o.OrderID FROM dbo.Orders o WHERE o.CustomerID = c.CustomerID ORDER BY o.OrderDate DESC) AS last
OUTER APPLY STRING_SPLIT(c.Tags, ',') AS s;
