-- LEFT, RIGHT and FULL OUTER joins chained left-deep
SELECT *
FROM a
LEFT JOIN b ON b.id = a.id
RIGHT OUTER JOIN c ON c.id = b.id
FULL JOIN d ON d.id = c.id;
