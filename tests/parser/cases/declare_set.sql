-- DECLARE with initial values, SET and SELECT assignment, and a SET option
DECLARE @from date = '2026-01-01', @n int;
SET @n = (SELECT COUNT(*) FROM dbo.Orders WHERE OrderDate >= @from);
SELECT @n = @n + 1;
SET NOCOUNT ON;
