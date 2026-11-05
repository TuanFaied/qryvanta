-- A read-only login for tests and manual debugging.
-- The password comes from the sqlcmd variable ReaderPassword (see load.sh).
-- Qryvanta's docs recommend a read-only login as the only reliable read-only guarantee;
-- this login lets tests prove that writes are rejected by the server.
SET NOCOUNT ON;
USE master;
GO

IF SUSER_ID(N'qryvanta_reader') IS NOT NULL
    DROP LOGIN qryvanta_reader;
GO

CREATE LOGIN qryvanta_reader
    WITH PASSWORD = N'$(ReaderPassword)',
         DEFAULT_DATABASE = QryvantaSample,
         CHECK_EXPIRATION = OFF;
GO

USE QryvantaSample;
GO

CREATE USER qryvanta_reader FOR LOGIN qryvanta_reader;
ALTER ROLE db_datareader ADD MEMBER qryvanta_reader;
-- SHOWPLAN lets the login read estimated execution plans (Phase 6) without executing writes.
GRANT SHOWPLAN TO qryvanta_reader;
-- VIEW DEFINITION makes procedures and functions visible in the schema explorer; without it
-- SQL Server hides their metadata from a db_datareader-only login. It grants no execution.
GRANT VIEW DEFINITION TO qryvanta_reader;
GO
