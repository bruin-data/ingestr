# Chess.com

[Chess.com](https://www.chess.com/) is an online platform offering chess games, tournaments, lessons, and more.

ingestr supports Chess.com as a source.

## URI Format

The URI format for Chess is as follows:

```plaintext
--source-uri 'chess://?players_username=<List[str]>'
```

URI parameter:

- `players_username`: A list of players usernames for which you want to fetch data. If no usernames are provided, then data of 10 different players will be fetched.

The URI is used to connect to the Chess.com API for extracting data. More details on setting up Chess integrations can be found [here](https://www.chess.com/news/view/published-data-api).

## Setting up a Chess Integration

Let's say you have a list of player usernames: max2 and peter23. Here's a sample command that will copy the data from Chess into a DuckDB database:

```sh
ingestr ingest --source-uri 'chess://?players_username=max2,peter23' --source-table 'players_profiles' --dest-uri 'duckdb:///chess.duckdb' --dest-table 'players.profiles'
```

The result of this command will be a table in the `chess.duckdb` database.

## Available Tables

Chess source allows ingesting the following sources into separate tables:

- `players_profiles`: Retrives player profiles based on a list of player usernames.
- `players_games`: Retrives players' games for specified players.
- `players_archives`: Retrives url to game archives for specified players.
- `players_online_status`: Retrives players' online status for specified players.

Use these as `--source-table` parameter in the `ingestr ingest` command.
