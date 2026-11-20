-- Unicode identifiers and N-string literals with characters outside the BMP (UTF-16 offsets)
SELECT [Kunde GrÃ¶ÃŸe], N'æ±äº¬ ðŸ˜' AS Stadt, 'naÃ¯ve' AS ansi
FROM dbo.[Bestellungen â‚¬] AS b
WHERE b.Ort = N'ZÃ¼rich';
