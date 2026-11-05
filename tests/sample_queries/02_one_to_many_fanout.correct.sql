-- Fix: aggregate each one-to-many relationship separately, then join the totals.
WITH paid AS (
    SELECT o.CustomerID, SUM(p.Amount) AS TotalPaid
    FROM dbo.Orders AS o
    JOIN dbo.Payments AS p ON p.OrderID = o.OrderID
    GROUP BY o.CustomerID
), items AS (
    SELECT o.CustomerID, SUM(oi.Quantity) AS ItemsBought
    FROM dbo.Orders AS o
    JOIN dbo.OrderItems AS oi ON oi.OrderID = o.OrderID
    GROUP BY o.CustomerID
)
SELECT c.CustomerID, c.Name, COALESCE(paid.TotalPaid, 0) AS TotalPaid, items.ItemsBought
FROM dbo.Customers AS c
JOIN items ON items.CustomerID = c.CustomerID
LEFT JOIN paid ON paid.CustomerID = c.CustomerID
ORDER BY c.CustomerID;
