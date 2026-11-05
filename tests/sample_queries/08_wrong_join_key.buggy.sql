-- Question: the lines of order 103 with their product names.
-- Bug: the join compares ProductID with OrderItemID. Both are integers in the same range, so
-- the query runs, returns plausible-looking names, and silently drops a line.
SELECT oi.OrderItemID, p.Name AS Product, oi.Quantity
FROM dbo.OrderItems AS oi
JOIN dbo.Products AS p ON p.ProductID = oi.OrderItemID
WHERE oi.OrderID = 103
ORDER BY oi.OrderItemID;
