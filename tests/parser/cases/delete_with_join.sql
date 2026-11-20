-- DELETE with FROM and JOIN, and a plain DELETE with WHERE
DELETE l FROM dbo.OrderLines l JOIN dbo.Orders o ON o.OrderID = l.OrderID WHERE o.Status = 'Cancelled';
DELETE FROM dbo.Log WHERE LoggedAt < '2025-01-01';
