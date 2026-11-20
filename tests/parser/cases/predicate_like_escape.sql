-- LIKE, NOT LIKE and an ESCAPE character
SELECT * FROM dbo.Products
WHERE Name LIKE N'Desk%' AND Code NOT LIKE '50!%%' ESCAPE '!';
