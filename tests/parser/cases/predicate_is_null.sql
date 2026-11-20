-- IS NULL and IS NOT NULL, plus the = NULL comparison mistake
SELECT * FROM dbo.Employees
WHERE DepartmentID IS NULL OR ManagerID IS NOT NULL OR Email = NULL;
