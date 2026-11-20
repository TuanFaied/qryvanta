-- ROW_NUMBER with PARTITION BY and ORDER BY in OVER
SELECT CustomerID, OrderID,
       ROW_NUMBER() OVER (PARTITION BY CustomerID ORDER BY OrderDate DESC, OrderID) AS rn
FROM dbo.Orders;
