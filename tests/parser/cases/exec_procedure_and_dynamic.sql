-- EXEC of a procedure with parameters, EXECUTE with a return value, and dynamic SQL
EXEC dbo.usp_Report @from = '2026-01-01', @to = '2026-01-31';
DECLARE @rc int;
EXECUTE @rc = dbo.usp_Check 5;
EXEC (N'SELECT 1');
EXEC sp_executesql N'SELECT @x', N'@x int', @x = 1;
