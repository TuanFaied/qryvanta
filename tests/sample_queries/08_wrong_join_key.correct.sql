-- Fix: join on the foreign key column.
SELECT oi.OrderItemID, p.Name AS Product, oi.Quantity
FROM dbo.OrderItems AS oi
JOIN dbo.Products AS p ON p.ProductID = oi.ProductID
WHERE oi.OrderID = 103
ORDER BY oi.OrderItemID;
