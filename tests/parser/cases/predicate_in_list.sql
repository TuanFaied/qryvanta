-- IN and NOT IN with value lists
SELECT * FROM dbo.Orders
WHERE Status IN ('Shipped', 'Delivered') AND ShipCountry NOT IN ('XX', 'YY', 'ZZ');
