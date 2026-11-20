-- a chain of CTEs where each uses the previous one
WITH paid AS (
    SELECT CustomerID, SUM(Amount) AS Total FROM dbo.Payments GROUP BY CustomerID
), big AS (
    SELECT CustomerID FROM paid WHERE Total > 1000
)
SELECT c.Name FROM dbo.Customers c JOIN big ON big.CustomerID = c.CustomerID;
