# MySQL
MySQL is an open source relational database management system, known for its speed and reliability.

ingestr supports MySQL as a source.

## URI format
The URI format for MySQL is as follows:

```plaintext
mysql://user:password@host:port/dbname 
```

URI parameters:
- `user`: the user name to connect to the database
- `password`: the password for the user
- `host`: the host address of the database server
- `port`: the port number the database server is listening on, the default is 3306
- `dbname`: the name of the database to connect to

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's MySQL dialect [here](https://docs.sqlalchemy.org/en/20/core/engines.html#mysql).
