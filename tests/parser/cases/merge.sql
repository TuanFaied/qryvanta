-- MERGE is classified with its target (its WHEN clauses are not modelled yet)
MERGE dbo.Stock AS t
USING dbo.Deliveries AS s ON s.ProductID = t.ProductID
WHEN MATCHED THEN UPDATE SET t.Qty = t.Qty + s.Qty
WHEN NOT MATCHED THEN INSERT (ProductID, Qty) VALUES (s.ProductID, s.Qty);
