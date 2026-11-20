-- a comma list of three tables becomes left-deep comma joins without predicates
SELECT * FROM dbo.Orders o, dbo.Customers c, dbo.Products p;
