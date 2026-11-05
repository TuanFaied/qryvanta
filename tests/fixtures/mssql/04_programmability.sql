-- Views, procedures and functions so the schema explorer has every object kind to show.
-- They are not used by the sample queries.
SET NOCOUNT ON;
USE QryvantaSample;
GO

CREATE VIEW dbo.vw_OrderTotals
AS
SELECT oi.OrderID, SUM(oi.Quantity * oi.UnitPrice) AS OrderTotal
FROM dbo.OrderItems AS oi
GROUP BY oi.OrderID;
GO

CREATE PROCEDURE dbo.usp_CustomerOrders
    @CustomerID int
AS
BEGIN
    SET NOCOUNT ON;
    SELECT o.OrderID, o.OrderDate, o.Status
    FROM dbo.Orders AS o
    WHERE o.CustomerID = @CustomerID
    ORDER BY o.OrderDate;
END;
GO

CREATE FUNCTION dbo.fn_OrderTotal (@OrderID int)
RETURNS decimal(12,2)
AS
BEGIN
    RETURN (SELECT SUM(Quantity * UnitPrice) FROM dbo.OrderItems WHERE OrderID = @OrderID);
END;
GO
