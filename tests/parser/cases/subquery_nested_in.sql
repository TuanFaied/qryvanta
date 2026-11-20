-- IN with a subquery that itself filters with IN
SELECT p.Name
FROM dbo.Products p
WHERE p.ProductID IN (SELECT l.ProductID FROM dbo.OrderLines l
                      WHERE l.OrderID IN (SELECT o.OrderID FROM dbo.Orders o WHERE o.Status = 'Open'));
