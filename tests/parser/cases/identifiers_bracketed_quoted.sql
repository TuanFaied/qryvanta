-- bracketed and double-quoted identifiers with spaces, keywords and an escaped bracket
SELECT [Order ID], "Customer Name", [Select], [a]]b]
FROM [Sales Data].[dbo].[Order Details] AS [od];
