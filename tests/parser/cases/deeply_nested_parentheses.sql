-- nested parentheses around scalars and predicates collapse into flags on the inner nodes
SELECT ((a)) FROM t WHERE (((a = 1)) AND ((b = 2) OR (c = 3)));
