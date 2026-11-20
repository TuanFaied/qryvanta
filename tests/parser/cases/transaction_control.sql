-- BEGIN TRAN, SAVE, ROLLBACK to a savepoint and COMMIT are transaction statements
BEGIN TRANSACTION;
UPDATE dbo.Orders SET Status = 'Hold' WHERE OrderID = 1;
SAVE TRANSACTION before_delete;
DELETE FROM dbo.Orders WHERE OrderID = 2;
ROLLBACK TRANSACTION before_delete;
COMMIT;
