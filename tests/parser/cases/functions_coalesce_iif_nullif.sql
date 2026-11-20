-- COALESCE, ISNULL, NULLIF, IIF, schema-qualified UDFs and parameterless CURRENT_TIMESTAMP
SELECT COALESCE(Email, Phone, 'n/a'), ISNULL(Email, ''), NULLIF(Qty, 0),
       IIF(Qty > 0, 'in stock', 'out'), dbo.fn_Score(CustomerID), CURRENT_TIMESTAMP
FROM dbo.Customers;
