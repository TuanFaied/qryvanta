-- Data for the QryvantaSample database. Every value here is synthetic.
-- The comments mark the rows that make each scenario in tests/sample_queries misbehave.
SET NOCOUNT ON;
USE QryvantaSample;
GO

INSERT INTO dbo.Departments (DepartmentID, Name) VALUES
    (1, N'Engineering'),
    (2, N'Finance'),
    (3, N'Sales'),
    (4, N'Marketing'),
    (5, N'Legal');          -- no employees

INSERT INTO dbo.Employees (EmployeeID, Name, DepartmentID, HireDate) VALUES
    (1,  N'Amal Fernando',  1,    '2021-03-01'),
    (2,  N'Bilal Rahman',   1,    '2022-07-15'),
    (3,  N'Chen Wei',       1,    '2023-01-09'),
    (4,  N'Dilani Perera',  2,    '2020-11-23'),
    (5,  N'Erik Larsen',    2,    '2024-02-05'),
    (6,  N'Fatima Noor',    3,    '2019-06-17'),
    (7,  N'Gihan Silva',    3,    '2022-09-30'),
    (8,  N'Hana Sato',      4,    '2021-12-01'),
    (9,  N'Ishan Gupta',    4,    '2025-04-14'),
    (10, N'Jonas Weber',    NULL, '2025-08-01'),  -- not yet assigned to a department
    (11, N'Kavya Menon',    NULL, '2025-10-20'),  -- not yet assigned to a department
    (12, N'Leo Martin',     99,   '2018-05-07');  -- department 99 no longer exists

INSERT INTO dbo.Customers (CustomerID, Name, Email, Country) VALUES
    (1, N'Nimal Perera', N'nimal@example.com',     'LK'),
    (2, N'Sara Khan',    N'sara@example.com',      'LK'),
    (3, N'Tom Baker',    N'tom@example.com',       'GB'),
    (4, N'Ana Silva',    N'ana@example.com',       'BR'),
    (5, N'NIMAL PERERA', N' Nimal@Example.com ',   'LK'),  -- duplicate of 1 (case and spaces)
    (6, N'Sara Khan',    N'SARA@example.com',      'LK'),  -- duplicate of 2
    (7, N'Yuki Tanaka',  N'yuki@example.com',      'JP'),  -- never ordered
    (8, N'Omar Haddad',  NULL,                     'AE');  -- never ordered, no email

INSERT INTO dbo.Products (ProductID, Name, ListPrice) VALUES
    (1, N'Notebook',     10.00),
    (2, N'Pen set',      25.00),
    (3, N'Sticky notes',  5.00),
    (4, N'Desk lamp',    40.00),
    (5, N'Office chair', 100.00);

INSERT INTO dbo.Orders (OrderID, CustomerID, OrderDate, Status, ShipCountry) VALUES
    (101, 1,    '2026-01-05T09:15:00', 'Delivered', 'LK'),
    (102, 1,    '2026-01-20T14:00:00', 'Shipped',   'LK'),
    (103, 2,    '2026-01-31T16:45:00', 'Delivered', 'LK'),  -- last day of January, afternoon
    (104, 3,    '2026-01-31T00:00:00', 'Shipped',   'GB'),  -- last day of January, midnight
    (105, 4,    '2026-02-01T00:00:00', 'Delivered', 'BR'),  -- first instant of February, unpaid
    (106, 3,    '2026-02-10T11:30:00', 'Delivered', 'GB'),
    (107, NULL, '2026-02-12T10:00:00', 'Cancelled', NULL),  -- guest checkout: no customer
    (108, 2,    '2026-02-14T08:00:00', 'Shipped',   'IN');

-- Order totals: 101 = 45, 102 = 15, 103 = 115, 104 = 100, 105 = 20, 106 = 90, 107 = 25, 108 = 200.
INSERT INTO dbo.OrderItems (OrderItemID, OrderID, ProductID, Quantity, UnitPrice) VALUES
    (1,  101, 1, 2,  10.00),
    (2,  101, 2, 1,  25.00),
    (3,  102, 3, 3,   5.00),
    (4,  103, 1, 1,  10.00),
    (5,  103, 4, 2,  40.00),
    (6,  103, 2, 1,  25.00),
    (7,  104, 5, 1, 100.00),
    (8,  105, 3, 4,   5.00),
    (9,  106, 4, 1,  40.00),
    (10, 106, 1, 5,  10.00),
    (11, 107, 2, 1,  25.00),
    (12, 108, 5, 2, 100.00);

INSERT INTO dbo.Payments (PaymentID, OrderID, Amount, PaidAt) VALUES
    (1, 101,  20.00, '2026-01-05T09:20:00'),  -- 101 paid in two installments
    (2, 101,  25.00, '2026-01-12T10:00:00'),
    (3, 102,  15.00, '2026-01-20T14:05:00'),
    (4, 103, 115.00, '2026-01-31T16:50:00'),
    (5, 104, 100.00, '2026-01-31T00:10:00'),
    (6, 106,  90.00, '2026-02-10T11:35:00'),
    (7, 108, 100.00, '2026-02-14T08:05:00'),  -- 108 paid in two installments
    (8, 108, 100.00, '2026-02-28T08:05:00');
GO
