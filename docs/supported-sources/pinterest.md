# Pinterest

[ Pinterest](https://www.pinterest.com/) is a social media platform for discovering and sharing ideas using visual bookmarks called pins.

ingestr supports Pinterest as a source.

## URI format

The URI format for Pinterest is as follows:

```plaintext
pinterest:///?access_token=<access_token>
```
For more information
URI parameters:
- `access_token`: token used for authentication with the Pinterest API.

## Tables

The Pinterest source exposes the following tables:

- `pins`: Retrieves list of the Pins
- `boards`: Retrieves list of the boards

Use these table names with the `--source-table` parameter in the `ingestr ingest` command.