-- NOT binds tighter than AND, and NOT over a parenthesized OR
SELECT * FROM t WHERE NOT a = 1 AND NOT (b = 2 OR c = 3);
