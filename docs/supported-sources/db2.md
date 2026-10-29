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

## Status and known limitations

The DRDA/DDM wire protocol implementation and the Db2 for i (iSeries/AS400) catalog/handshake path have been exercised against a live production Db2 for i server, including CHAR/VARCHAR/VARGRAPHIC, DATE/TIME/TIMESTAMP, INTEGER/SMALLINT/BIGINT/FLOAT, packed and zoned DECIMAL/NUMERIC, multi-block result sets, and SECMEC 8 (password-substitute) authentication. Db2 for Linux/Unix/Windows and Db2 for z/OS use the same DRDA client and the same catalog-dialect switch, but that code path has only been exercised against a fake DRDA server in unit tests, not a live LUW or z/OS server - if you hit something, please open an issue with the platform and error.

Not yet supported:
- CLOB, DBCLOB, and BLOB columns delivered via an SDA (Self-Defining type) descriptor. These fail with a clear error naming the column rather than silently corrupting the row; exclude the column from the query as a workaround.

Known gaps in test coverage (works as implemented, but not independently confirmed against a live server capture):
- The VARGRAPHIC catalog type name truncation (`VARG`) is handled on the same evidence pattern as a confirmed case (`TIMESTMP`), but hasn't itself been confirmed against a fresh catalog query.
- SQLSTATE in the SQLCARD diagnostic structure is decoded as CCSID 500, consistent with every other DRDA protocol-metadata field, but hasn't been confirmed against a real non-ASCII SQLSTATE value.
