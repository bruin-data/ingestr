# SAP HANA
SAP HANA is an in-memory, column-oriented, relational database management system.

ingestr supports SAP HANA as both a source and destination. It uses the [SQLAlchemy connector for SAP HANA](https://github.com/SAP/sqlalchemy-hana/), so the connection options there would all be valid.

## URI format
The URI format for SAP HANA is as follows:

```plaintext
hana://user:password@host:port/dbname
```

URI parameters:
- `user`: the username to connect to the database
- `password`: the password for the user
- `host`: the host address of the database server
- `port`: the port number the database server is listening on, default is 30015
- `dbname`: the name of the database to connect to

The URI structure can be used both for sources and destinations. More details about SAP HANA’s JDBC and ODBC interfaces can be found [here](https://github.com/SAP/sqlalchemy-hana/).
