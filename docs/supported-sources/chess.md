# Chess.com

[Chess.com](https://www.chess.com/) is an online platform offering chess games, tournaments, lessons, and more.

ingestr supports Chess.com as a source, primarily to play around with the data of players, games, and more since it doesn't require any authentication.

## URI format

The URI format for Chess is as follows:

```plaintext
--source-uri 'chess://?players=<List[str]>'
```

URI parameter:

- `players`: A list of players usernames for which you want to fetch data. If no usernames are provided, then data of 4 different players will be fetched.

## Setting up a Chess Integration

Let's say you have a list of player usernames: max2 and peter23. Here's a sample command that will copy the data from Chess into a DuckDB database:

```sh
ingestr ingest --source-uri 'chess://?players=max2,peter23' --source-table 'profiles' --dest-uri 'duckdb:///chess.duckdb' --dest-table 'players.profiles'
```

The result of this command will be a table in the `chess.duckdb` database.

## Tables

Chess source allows ingesting the following sources into separate tables:

- `profiles`: Retrieves player profiles based on a list of player usernames.
- `games`: Retrieves players' games for specified players.
- `archives`: Retrieves the URLs to game archives for specified players.

Use these as `--source-table` parameter in the `ingestr ingest` command.
