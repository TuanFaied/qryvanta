-- Question: how many distinct customers do we have?
-- Bug: the same person was entered several times with different capitalisation and spacing.
SELECT COUNT(*) AS Customers
FROM dbo.Customers;
