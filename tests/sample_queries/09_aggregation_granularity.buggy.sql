-- Question: what is the average order value?
-- Bug: AVG runs over order lines, so this is the average line value, not the average order.
SELECT CAST(AVG(oi.Quantity * oi.UnitPrice) AS decimal(10,2)) AS AvgOrderValue
FROM dbo.OrderItems AS oi;
