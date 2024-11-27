---
title: Chess.com
description: dlt source for Chess.com API
keywords: [chess.com api, chess.com source, chess.com]
---


# Chess.com

[Chess.com](https://www.chess.com/) is an online platform that offers services for chess
enthusiasts. It includes online chess games tournaments, lessons, and more.

Resources that can be loaded using this verified source are:

| Name             | Description                                                            |
| ---------------- | ---------------------------------------------------------------------- |
| players_profiles | retrives player profiles for a list of player usernames                |
| players_archives | retrives url to game archives for specified players                    |
| players_games    | retrives players games that happened between start_month and end_month |


## Initialize the pipeline

```bash
dlt init chess duckdb
```

Here, we chose duckdb as the destination. Alternatively, you can also choose redshift, bigquery, or
any of the other [destinations](https://dlthub.com/docs/dlt-ecosystem/destinations/).

## Add credentials

1. [Chess.com API](https://www.chess.com/news/view/published-data-api) is a public API that does not
   require authentication or including secrets in `secrets.toml`.

2. Follow the instructions in the
   [destinations](https://dlthub.com/docs/dlt-ecosystem/destinations/) document to add credentials
   for your chosen destination.

## Run the pipeline

1. Install the necessary dependencies by running the following command:

   ```bash
   pip install -r requirements.txt
   ```

2. Now the pipeline can be run by using the command:

   ```bash
   python chess_pipeline.py
   ```

3. To make sure that everything is loaded as expected, use the command:

   ```bash
   dlt pipeline chess_pipeline show
   ```

💡 To explore additional customizations for this pipeline, we recommend referring to the official
`dlt` Chess documentation. It provides comprehensive information and guidance on how to further
customize and tailor the pipeline to suit your specific needs. You can find the `dlt` Chess
documentation in
[Setup Guide: Chess.com.](https://dlthub.com/docs/dlt-ecosystem/verified-sources/chess)
