-- Question: total paid and items bought per customer.
-- Bug: joining OrderItems and Payments to the same order multiplies rows (items x payments),
-- so both sums are inflated, and the inner join to Payments drops customers with unpaid orders.
SELECT c.CustomerID, c.Name, SUM(p.Amount) AS TotalPaid, SUM(oi.Quantity) AS ItemsBought
FROM dbo.Customers AS c
JOIN dbo.Orders AS o ON o.CustomerID = c.CustomerID
JOIN dbo.OrderItems AS oi ON oi.OrderID = o.OrderID
JOIN dbo.Payments AS p ON p.OrderID = o.OrderID
GROUP BY c.CustomerID, c.Name
ORDER BY c.CustomerID;
