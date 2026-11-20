-- LAG and LEAD with offsets and a default
SELECT OrderDate,
       LAG(Amount, 1, 0) OVER (ORDER BY OrderDate) AS PrevAmount,
       LEAD(Amount) OVER (ORDER BY OrderDate) AS NextAmount
FROM dbo.Payments;
