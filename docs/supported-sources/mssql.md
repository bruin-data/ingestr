# Microsoft SQL Server
Microsoft SQL Server is a relational database management system developed by Microsoft.

ingestr supports Microsoft SQL Server as both a source and destination.

## Installation

To use Microsoft SQL Server with ingestr, you need to install the `pyodbc` add-on as well. You can do this by running:

```bash
pip install ingestr[odbc]
```

## URI format
The URI format for Microsoft SQL Server is as follows:

```plaintext
mssql://user:password@host:port/dbname?driver=ODBC+Driver+18+for+SQL+Server&TrustServerCertificate=yes
```

URI parameters:
- `user`: the username to connect to the SQL Server instance
- `password`: the password to connect to the SQL Server instance
- `host`: the hostname of the SQL Server instance
- `port`: the port of the SQL Server instance
- `dbname`: the name of the database to connect to
- `driver`: the ODBC driver to use to connect to the SQL Server instance
- `TrustServerCertificate`: whether to trust the server certificate

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's SQL Server dialect [here](https://docs.sqlalchemy.org/en/20/core/engines.html#microsoft-sql-server).
