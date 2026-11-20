-- SELECT * and qualified t.* items
SELECT *, o.*, c.Name FROM dbo.Orders o JOIN dbo.Customers c ON c.CustomerID = o.CustomerID;
