-- an aggregate used as a window function with a ROWS frame
SELECT OrderID,
       SUM(Amount) OVER (ORDER BY OrderDate ROWS BETWEEN 6 PRECEDING AND CURRENT ROW) AS Rolling
FROM dbo.Payments;
