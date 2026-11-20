-- searched and simple CASE, CAST, CONVERT with a style, TRY_CAST and TRY_CONVERT
SELECT CASE WHEN Amount > 100 THEN 'big' WHEN Amount > 10 THEN 'medium' ELSE 'small' END AS size,
       CASE Status WHEN 'S' THEN 'Shipped' END AS label,
       CAST(Amount AS decimal(12, 2)) AS amt,
       CONVERT(varchar(10), OrderDate, 120) AS day,
       TRY_CAST(Code AS int) AS code,
       TRY_CONVERT(date, RawDate) AS raw
FROM dbo.Orders;
