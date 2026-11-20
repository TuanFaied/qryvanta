-- without parentheses AND binds tighter than OR
SELECT * FROM t WHERE a = 1 OR b = 2 AND c = 3 OR d = 4;
