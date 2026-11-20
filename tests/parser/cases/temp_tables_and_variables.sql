-- a local temp table, a global temp table and a table variable as sources
DECLARE @ids TABLE (id int);
SELECT * FROM #work w JOIN ##shared s ON s.id = w.id JOIN @ids i ON i.id = w.id;
