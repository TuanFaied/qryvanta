-- Question: list every employee with their department.
-- Bug: INNER JOIN drops employees whose DepartmentID is NULL or points to a missing department.
SELECT e.EmployeeID, e.Name, d.Name AS Department
FROM dbo.Employees AS e
INNER JOIN dbo.Departments AS d ON d.DepartmentID = e.DepartmentID
ORDER BY e.EmployeeID;
