-- a VALUES table constructor as a source with column aliases
SELECT v.code, v.label FROM (VALUES (1, 'one'), (2, 'two')) AS v (code, label);
