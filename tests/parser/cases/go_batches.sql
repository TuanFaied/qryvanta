-- GO separates batches; GO 3 repeats one; a batch of only comments is skipped
SELECT 1 AS first;
GO 3
-- nothing but a comment here
GO
select 'GO' AS not_a_separator
go
SELECT 2 AS last;
