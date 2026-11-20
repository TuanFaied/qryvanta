-- BETWEEN and NOT BETWEEN with low and high bounds
SELECT * FROM dbo.Orders
WHERE OrderDate BETWEEN '2026-01-01' AND '2026-01-31'
  AND Amount NOT BETWEEN 0 AND 10;
