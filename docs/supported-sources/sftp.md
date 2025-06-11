# SFTP

SFTP (SSH File Transfer Protocol) is a secure file transfer protocol that runs over the SSH protocol. It provides a secure way to transfer files between a local and a remote computer.

`ingestr` supports SFTP as a data source.

## URI Format

The URI for connecting to an SFTP server is structured as follows:

```plaintext
sftp://<username>:<password>@<host>:<port>
```

## URI Components:
- `username`: The username for the SFTP server.
- `password`: The password for the SFTP server.
- `host`: The hostname or IP address of the SFTP server.
- `port`: The port number of the SFTP server (defaults to 22 if not specified).

## Setting up an SFTP Integration

To integrate `ingestr` with an SFTP server, you need the server's hostname, port, a valid username, and a password.

Once you have your credentials, you can load data to desired destaination.

### Example: Loading data from SFTP TO DUCKDB

```sh
ingestr ingest \
    --source-uri 'sftp://myuser:MySecretPassword123@sftp.example.com' \
    --source-table 'user.csv' \
    --dest-uri duckdb:///sftp_data.duckdb \
    --dest-table 'dest.users_deatils'
```

<img alt="sftp" src="../media/sftp.png"/>


The `--source-table` specifies `/path/to/directory` The base directory on the server where `ingestr` should start looking for files.



