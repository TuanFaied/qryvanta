-- a recursive CTE (it references itself) with a column list and MAXRECURSION
WITH tree (EmployeeID, ManagerID, Depth) AS (
    SELECT EmployeeID, ManagerID, 0 FROM dbo.Employees WHERE ManagerID IS NULL
    UNION ALL
    SELECT e.EmployeeID, e.ManagerID, t.Depth + 1
    FROM dbo.Employees e JOIN tree t ON t.EmployeeID = e.ManagerID
)
SELECT * FROM tree OPTION (MAXRECURSION 50);
