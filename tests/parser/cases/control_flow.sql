-- IF/ELSE, WHILE and TRY/CATCH keep their nested statements
IF EXISTS (SELECT 1 FROM dbo.Orders WHERE Status = 'Open')
BEGIN
    PRINT 'open orders';
    EXEC dbo.usp_Notify;
END
ELSE
    SELECT 0 AS none;

WHILE @@ROWCOUNT > 0
    DELETE TOP (100) FROM dbo.Log WHERE LoggedAt < '2025-01-01';

BEGIN TRY
    SELECT 1 / 0;
END TRY
BEGIN CATCH
    SELECT ERROR_NUMBER() AS err;
END CATCH;
