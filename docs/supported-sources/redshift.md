# Redshift
Redshift is a fully managed, petabyte-scale data warehouse service in the cloud.

ingestr supports Redshift as both a source and destination.

## URI format
The URI format for Redshift is as follows:

```plaintext
redshift://user:password@host:port/dbname?sslmode=require
```

URI parameters:
- `username`: the user name to connect to the database
- `password`: the password for the user
- `host`: the host address of the database server
- `port`: the port number the database server is listening on, the default is 5439
- `database`: the name of the database to connect to
- `sslmode`: optional, the SSL mode to use when connecting to the database

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's Redshift dialect [here](https://aws.amazon.com/blogs/big-data/use-the-amazon-redshift-sqlalchemy-dialect-to-interact-with-amazon-redshift/).
