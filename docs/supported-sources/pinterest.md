# Pinterest

[Pinterest](https://www.pinterest.com/) is a social media platform for discovering and sharing ideas using visual bookmarks.

ingestr supports Pinterest as a source.


## URI Format

The URI format for Pinterest is as follows:

```plaintext
pinterest://?access_token=<access_token>
```

URI parameters:
- `access_token`: The token used for authentication with the Pinterest API. You can obtain an access token from the [official Pinterest documentation](https://developers.pinterest.com/docs/getting-started/connect-app/)


## Tables

Pinterest source allows ingesting the following sources into separate tables:

| Table           | PK | Inc Key | Inc Strategy | Details                                                                                                                                        |
| --------------- | ----------- | --------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| [pins](https://developers.pinterest.com/docs/api/v5/pins-list)          | id | created_at     | merge             | Retrieves a list of pins.|
| [boards](https://developers.pinterest.com/docs/api/v5/boards-list)      | id | created_at     | merge               | Retrieves a list of boards.
 |

Use these as `--source-table` parameter in the `ingestr ingest` command.
