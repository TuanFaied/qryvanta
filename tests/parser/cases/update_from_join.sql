-- UPDATE with FROM, a JOIN and WHERE; the target is the alias
UPDATE o SET o.Status = 'Late'
FROM dbo.Orders o
JOIN dbo.Shipments s ON s.OrderID = o.OrderID
WHERE s.ShippedDate > o.DueDate;
