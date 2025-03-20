# IBM Db2
[IBM Db2](https://www.ibm.com/db2) is a high-performance, enterprise-grade relational database system designed for reliability, scalability, and transactional integrity.

ingestr supports IBM Db2 as a source.

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

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's Db2 dialect [here](https://github.com/ibmdb/python-ibmdbsa?tab=readme-ov-file#connecting).
