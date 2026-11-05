-- Fix: LEFT JOIN keeps employees without a matching department.
SELECT e.EmployeeID, e.Name, COALESCE(d.Name, N'(no department)') AS Department
FROM dbo.Employees AS e
LEFT JOIN dbo.Departments AS d ON d.DepartmentID = e.DepartmentID
ORDER BY e.EmployeeID;
