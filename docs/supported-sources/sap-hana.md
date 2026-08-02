# SAP HANA
SAP HANA is an in-memory, column-oriented, relational database management system.

ingestr supports SAP HANA as both a source and destination. It uses SAP's pure-Go [`go-hdb`](https://github.com/SAP/go-hdb) driver and does not require SAP client libraries or cgo.

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

The same URI structure is used for both sources and destinations. HANA Cloud connections on port 443 enable TLS automatically, and `go-hdb` DSN parameters such as `databaseName` can be supplied as query parameters when needed.

The destination supports `replace`, `append`, `merge`, `delete+insert`, and `scd2` strategies. Tables may be specified as either `table` or `schema.table`. SCD2 logical primary keys must use bounded, non-LOB types. Other LOB-backed string, JSON, array, and binary columns can be compared when current and staged values are at most 2000 bytes; larger LOB values require a different strategy.
