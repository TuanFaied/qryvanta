-- Schema for the QryvantaSample database.
--
-- Some referential constraints are deliberately missing so that the data can contain the
-- problems Qryvanta is built to explain (orphaned foreign keys, NULL keys, duplicates).
-- See tests/sample_queries/README.md for the scenario each table supports.
SET NOCOUNT ON;
USE QryvantaSample;
GO

CREATE TABLE dbo.Departments (
    DepartmentID int           NOT NULL CONSTRAINT PK_Departments PRIMARY KEY,
    Name         nvarchar(50)  NOT NULL
);

-- No foreign key to Departments: employees may have no department or an unknown one.
CREATE TABLE dbo.Employees (
    EmployeeID   int           NOT NULL CONSTRAINT PK_Employees PRIMARY KEY,
    Name         nvarchar(100) NOT NULL,
    DepartmentID int           NULL,
    HireDate     date          NOT NULL
);

-- No unique constraint on Email: the same person was entered more than once.
CREATE TABLE dbo.Customers (
    CustomerID int           NOT NULL CONSTRAINT PK_Customers PRIMARY KEY,
    Name       nvarchar(100) NOT NULL,
    Email      nvarchar(200) NULL,
    Country    char(2)       NOT NULL
);

CREATE TABLE dbo.Products (
    ProductID int           NOT NULL CONSTRAINT PK_Products PRIMARY KEY,
    Name      nvarchar(100) NOT NULL,
    ListPrice decimal(10,2) NOT NULL
);

-- CustomerID is nullable: guest checkouts have no customer.
-- OrderDate includes a time of day, which matters for date-range filters.
CREATE TABLE dbo.Orders (
    OrderID     int          NOT NULL CONSTRAINT PK_Orders PRIMARY KEY,
    CustomerID  int          NULL CONSTRAINT FK_Orders_Customers REFERENCES dbo.Customers (CustomerID),
    OrderDate   datetime2(0) NOT NULL,
    Status      varchar(20)  NOT NULL,
    ShipCountry char(2)      NULL
);

CREATE TABLE dbo.OrderItems (
    OrderItemID int           NOT NULL CONSTRAINT PK_OrderItems PRIMARY KEY,
    OrderID     int           NOT NULL CONSTRAINT FK_OrderItems_Orders REFERENCES dbo.Orders (OrderID),
    ProductID   int           NOT NULL CONSTRAINT FK_OrderItems_Products REFERENCES dbo.Products (ProductID),
    Quantity    int           NOT NULL,
    UnitPrice   decimal(10,2) NOT NULL
);

-- An order can be paid in several installments.
CREATE TABLE dbo.Payments (
    PaymentID int           NOT NULL CONSTRAINT PK_Payments PRIMARY KEY,
    OrderID   int           NOT NULL CONSTRAINT FK_Payments_Orders REFERENCES dbo.Orders (OrderID),
    Amount    decimal(10,2) NOT NULL,
    PaidAt    datetime2(0)  NOT NULL
);
GO
