-- Question: number of orders and units bought per customer.
-- Bug: after joining OrderItems there is one row per order line, so COUNT(o.OrderID) counts
-- lines, not orders.
SELECT o.CustomerID, COUNT(o.OrderID) AS Orders, SUM(oi.Quantity) AS Units
FROM dbo.Orders AS o
JOIN dbo.OrderItems AS oi ON oi.OrderID = o.OrderID
WHERE o.CustomerID IS NOT NULL
GROUP BY o.CustomerID
ORDER BY o.CustomerID;
