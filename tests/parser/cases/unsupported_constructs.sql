-- constructs the model does not describe yet are kept as unsupported, never dropped
SELECT j.a
FROM OPENJSON(@json) WITH (a int '$.a') AS j
WHERE CONTAINS(j.a, 'x');
