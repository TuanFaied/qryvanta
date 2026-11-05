-- Fix: total each order first, then average the order totals.
SELECT CAST(AVG(t.OrderTotal) AS decimal(10,2)) AS AvgOrderValue
FROM (
    SELECT OrderID, SUM(Quantity * UnitPrice) AS OrderTotal
    FROM dbo.OrderItems
    GROUP BY OrderID
) AS t;
