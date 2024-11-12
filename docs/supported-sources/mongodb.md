# MongoDB
MongoDB is a popular, open source NoSQL database known for its flexibility, scalability, and wide adoption in a variety of applications.

ingestr supports MongoDB as a source.

## URI format
The URI format for MongoDB is as follows:

```plaintext
mongodb://user:password@host:port
```

URI parameters:
- `user`: the user name to connect to the database
- `password`: the password for the user
- `host`: the host address of the database server
- `port`: the port number the database server is listening on, default is 27017 for MongoDB


> [!CAUTION]
> Do not put the database name at the end of the URI for MongoDB, instead make it a part of `--source-table` option as `database.collection` format.


You can read more about MongoDB's connection string format [here](https://docs.mongodb.com/manual/reference/connection-string/).
