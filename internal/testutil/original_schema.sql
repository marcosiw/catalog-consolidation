-- Schema of the catalog.db provided with the assessment, before any migration.
CREATE TABLE Product (Id INTEGER PRIMARY KEY AUTOINCREMENT, Name TEXT NOT NULL, Brand TEXT, Category TEXT);
CREATE TABLE SellerProduct (Id INTEGER PRIMARY KEY AUTOINCREMENT, SellerName TEXT NOT NULL, ProductId INTEGER CONSTRAINT FK_Product_Id REFERENCES Product (Id) NOT NULL, SellerProductId INTEGER NOT NULL);
