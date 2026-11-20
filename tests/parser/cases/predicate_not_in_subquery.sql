-- NOT IN with a subquery (the classic NULL trap)
SELECT c.CustomerID FROM dbo.Customers c
WHERE c.CustomerID NOT IN (SELECT o.CustomerID FROM dbo.Orders o);
