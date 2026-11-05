-- Fix: COUNT(DISTINCT o.OrderID) counts each order once. Units are at line grain already.
SELECT o.CustomerID, COUNT(DISTINCT o.OrderID) AS Orders, SUM(oi.Quantity) AS Units
FROM dbo.Orders AS o
JOIN dbo.OrderItems AS oi ON oi.OrderID = o.OrderID
WHERE o.CustomerID IS NOT NULL
GROUP BY o.CustomerID
ORDER BY o.CustomerID;
