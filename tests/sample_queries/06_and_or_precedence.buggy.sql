-- Question: shipped or delivered orders going to Sri Lanka (LK).
-- Bug: AND binds tighter than OR, so this reads as
--   Status = 'Shipped' OR (Status = 'Delivered' AND ShipCountry = 'LK')
-- and returns shipped orders to every country.
SELECT OrderID, Status, ShipCountry
FROM dbo.Orders
WHERE Status = 'Shipped' OR Status = 'Delivered' AND ShipCountry = 'LK'
ORDER BY OrderID;
