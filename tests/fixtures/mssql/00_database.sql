-- Recreates the QryvantaSample database from scratch. Safe to run repeatedly.
-- Development and test use only: this drops the database if it exists.
SET NOCOUNT ON;

IF DB_ID(N'QryvantaSample') IS NOT NULL
BEGIN
    ALTER DATABASE QryvantaSample SET SINGLE_USER WITH ROLLBACK IMMEDIATE;
    DROP DATABASE QryvantaSample;
END;
GO

CREATE DATABASE QryvantaSample;
GO
