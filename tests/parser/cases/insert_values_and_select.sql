-- INSERT with VALUES and INSERT ... SELECT with a column list
INSERT INTO dbo.Log (Message) VALUES (N'start');
INSERT dbo.Archive (OrderID, Amount) SELECT OrderID, Amount FROM dbo.Orders WHERE Status = 'Closed';
