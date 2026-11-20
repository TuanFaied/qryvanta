-- CREATE VIEW and CREATE PROCEDURE must each be alone in their batch
CREATE VIEW dbo.vOpenOrders AS SELECT OrderID FROM dbo.Orders WHERE Status = 'Open';
GO
CREATE PROCEDURE dbo.usp_Close @id int AS UPDATE dbo.Orders SET Status = 'Closed' WHERE OrderID = @id;
