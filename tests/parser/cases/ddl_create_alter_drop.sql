-- CREATE, ALTER and DROP of a table, a view and an index are all ddl
CREATE TABLE dbo.T (id int NOT NULL PRIMARY KEY, name nvarchar(50) NULL);
ALTER TABLE dbo.T ADD created datetime2 NOT NULL DEFAULT SYSDATETIME();
CREATE INDEX IX_T_name ON dbo.T (name);
DROP INDEX IX_T_name ON dbo.T;
DROP TABLE IF EXISTS dbo.T;
