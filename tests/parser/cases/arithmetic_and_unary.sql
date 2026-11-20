-- arithmetic precedence, a parenthesized sum, unary minus and string concatenation
SELECT (Price + Tax) * Qty - Discount / 2 % 3 AS total, -Qty AS neg, FirstName + ' ' + LastName AS full_name
FROM dbo.Lines;
