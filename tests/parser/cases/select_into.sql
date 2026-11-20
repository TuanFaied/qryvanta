-- SELECT ... INTO creates a table: the INTO target is recorded
SELECT o.OrderID, o.Amount INTO #big_orders FROM dbo.Orders o WHERE o.Amount > 1000;
