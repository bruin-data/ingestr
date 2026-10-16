# IBM Db2
[IBM Db2](https://www.ibm.com/db2) is a high-performance, enterprise-grade relational database system designed for reliability, scalability, and transactional integrity.

ingestr supports IBM Db2 as a source.

## Installation

Db2 source support is built into the ingestr binary. It does not require the IBM CLI driver, CGO, or a separate Python add-on.

## URI format
The URI format for DB2 is as follows:

```plaintext
db2://<username>:<password>@<host>:<port>/<database-name>
```

URI parameters:
- `username`: The username to connect to the database
- `password`: The password for the user
- `host`: The host address of the database server
- `port`: The port number the database server is listening
- `database-name`: the name of the database to connect to

Optional query parameters:
- `schema`: default schema to use for unqualified table names. If omitted, ingestr uses the username uppercased.
- `ssl`: set to `true` or `1` to connect with TLS.
- `timeout`: connection/read timeout in seconds.
- `platform`: forces which Db2 catalog dialect is used when reading table metadata. One of `luw`, `zos`, or `ibmi`. ingestr auto-detects this from the server's PRDID during the connection handshake, so this is only needed if detection picks the wrong dialect.
- `rdb_name` (Db2 for i only): overrides the real RDB directory name used for internal package lookups. ingestr auto-detects this from the server's handshake response, so this is normally not needed - see note below.

## Platform support

ingestr's Db2 client speaks DRDA directly, the wire protocol shared by every Db2 platform, so the same `db2://` URI works against:
- Db2 for Linux/Unix/Windows (LUW)
- Db2 for z/OS
- **Db2 for i (iSeries/AS400)**

The platform is detected automatically from the server's product ID during the handshake and determines which system catalog (`SYSCAT.*` for LUW/z/OS, `QSYS2.*` for i) is used to read column and primary key metadata.

For Db2 for i, `database` is commonly a symbolic alias such as `*LOCAL` (resolved by the server during the connection itself), but ingestr's internal package lookups need the real RDB directory name instead (the value shown by `WRKRDBDIRE` on the host). ingestr auto-detects this from the handshake response, but if that ever fails, set it explicitly with `rdb_name`, e.g. `db2://user:pass@host:446/%2ALOCAL?platform=ibmi&rdb_name=RDBNAME2`.
