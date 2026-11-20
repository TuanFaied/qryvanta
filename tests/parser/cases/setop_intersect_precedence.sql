-- INTERSECT binds tighter than EXCEPT; a parenthesized side is marked
SELECT id FROM a
EXCEPT
SELECT id FROM b INTERSECT SELECT id FROM c
UNION
(SELECT id FROM d);
