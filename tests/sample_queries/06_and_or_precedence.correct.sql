-- Fix: group the OR explicitly (or use IN).
SELECT OrderID, Status, ShipCountry
FROM dbo.Orders
WHERE Status IN ('Shipped', 'Delivered') AND ShipCountry = 'LK'
ORDER BY OrderID;
