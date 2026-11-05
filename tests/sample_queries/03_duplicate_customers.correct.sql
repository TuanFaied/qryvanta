-- Fix: count normalised email addresses. Customers without an email count individually,
-- because COUNT(DISTINCT ...) ignores NULLs.
SELECT COUNT(DISTINCT COALESCE(LOWER(TRIM(Email)), N'id:' + CAST(CustomerID AS nvarchar(10)))) AS Customers
FROM dbo.Customers;
