-- UNION vs UNION ALL; ORDER BY belongs to the whole set operation
SELECT Name FROM dbo.Customers
UNION
SELECT Name FROM dbo.Suppliers
UNION ALL
SELECT Name FROM dbo.Partners
ORDER BY Name;
