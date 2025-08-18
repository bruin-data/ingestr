# MotherDuck
MotherDuck is a managed cloud service built on DuckDB, designed for fast analytics and data processing in the cloud.

ingestr supports MotherDuck as both a source and destination.

## URI format
The URI format for MotherDuck is as follows:

```plaintext
motherduck://<database-name>?token=<your-token>
```

Alternatively, you can use the `md://` scheme:
```plaintext
md://<database-name>?token=<your-token>
```

URI parameters:
- `database-name`: the name of your MotherDuck database (optional, can be omitted for default connection)
- `token`: your MotherDuck authentication token

## Authentication

### Using Token in URI
Include the token directly in the URI:
```plaintext
md://<database-name>?token=<your-token>
```

### Connection without Database Name
If you want to connect without specifying a specific database:
```plaintext
md://?token=<your-token>
```

## Getting Your Token

1. Go to the MotherDuck UI
2. Click on your organization name in the top left and select "Settings"
3. Click "+ Create token"
4. Specify a name for the token
5. Choose between Read/Write or Read Scaling token type
6. Set expiration if desired and click "Create token"
7. Copy the generated token

The same URI structure can be used both for sources and destinations. You can read more about MotherDuck's connection options in their [official documentation](https://motherduck.com/docs/key-tasks/authenticating-and-connecting-to-motherduck/). 