-- three-part and four-part (linked server) object names
SELECT *
FROM Sales.dbo.Orders o
JOIN LinkedSrv.Archive.dbo.Orders a ON a.OrderID = o.OrderID;
