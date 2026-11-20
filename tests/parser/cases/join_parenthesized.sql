-- a parenthesized join on the right of another join keeps its own ON
SELECT *
FROM a
LEFT JOIN (b INNER JOIN c ON c.bid = b.id) ON b.aid = a.id;
