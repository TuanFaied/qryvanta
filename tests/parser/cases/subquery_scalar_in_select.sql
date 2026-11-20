-- a scalar subquery in the select list
SELECT c.CustomerID,
       (SELECT COUNT(*) FROM dbo.Orders o WHERE o.CustomerID = c.CustomerID) AS Orders
FROM dbo.Customers c;
