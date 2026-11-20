-- old-style join: the join condition sits in WHERE, the comma join itself has none
SELECT o.OrderID, c.Name
FROM dbo.Orders o, dbo.Customers c
WHERE c.CustomerID = o.CustomerID AND o.Status = 'Shipped';
