-- a CTE named Orders: the unqualified name is the CTE, dbo.Orders is the table
WITH Orders AS (SELECT 1 AS OrderID)
SELECT * FROM Orders AS cte JOIN dbo.Orders AS real ON real.OrderID = cte.OrderID;
