-- comparisons with ANY and ALL over a subquery
SELECT * FROM dbo.Products
WHERE Price > ALL (SELECT Price FROM dbo.Discounts) OR Price = ANY (SELECT Price FROM dbo.Promotions);
