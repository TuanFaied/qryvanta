-- INNER JOIN with an ON predicate, aliases with and without AS
SELECT o.OrderID, c.Name
FROM dbo.Orders AS o
INNER JOIN dbo.Customers c ON c.CustomerID = o.CustomerID;
