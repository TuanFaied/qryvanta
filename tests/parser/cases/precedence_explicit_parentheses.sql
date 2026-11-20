-- explicit parentheses change the tree and are recorded on the grouped node
SELECT * FROM t WHERE (a = 1 OR b = 2) AND (c = 3);
