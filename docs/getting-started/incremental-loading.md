---
outline: deep
---

# Incremental Loading
ingestr supports incremental loading, which means you can choose to append, merge or delete+insert data into the destination table. Incremental loading allows you to ingest only the new rows from the source table into the destination table, which means that you don't have to ingest the entire table every time you run ingestr.

Before you use incremental loading, you should understand 3 important keys:
- `primary_key`: the column or columns that uniquely identify a row in the table, if you give a primary key for an ingestion the resulting rows will be deduplicated based on the primary key, which means there will only be one row for each primary key in the destination.
- `incremental_key`: the column that will be used to determine the new rows, if you give an incremental key for an ingestion the resulting rows will be filtered based on the incremental key, which means only the new rows will be ingested.
  - A good example of an incremental key is a timestamp column, where you only want to ingest the rows that are newer than the last ingestion, for example `created_at` or `updated_at`.
- `strategy`: the strategy to use for incremental loading, the available strategies are:
  - `replace`: replace the existing destination table with the source directly, this is the default strategy and the simplest one.
    - This strategy isn't recommended for large tables, as it will replace the entire table and can be slow.
  - `append`: simply append the new rows to the destination table.
  - `merge`: merge the new rows with the existing rows in the destination table, insert the new ones and update the existing ones with the new values.
  - `delete+insert`: delete the existing rows in the destination table that match the incremental key and then insert the new rows.



## Replace
Replace is the default strategy, and it simply replaces the entire destination table with the source table.

The following example below will replace the entire `my_schema.some_data` table in BigQuery with the `my_schema.some_data` table in Postgres.
```bash
ingestr ingest \
    --source-uri 'postgresql://admin:admin@localhost:8837/web?sslmode=disable' \
    --source-table 'my_schema.some_data' \
    --dest-uri 'bigquery://<your-project-name>?credentials_path=/path/to/service/account.json' \
```

Here's how the replace strategy works:
- The source table is downloaded.
- The source table is uploaded to the destination, replacing the destination table.


> [!CAUTION]
> This strategy will delete the entire destination table and replace it with the source table, use with caution.

## Append
Append will simply append the new rows from the source table to the destination table. By default, it will append all the rows. You should provide an `incremental_key` to use it as an incremental strategy.

The following example below will append the new rows from the `my_schema.some_data` table in Postgres to the `my_schema.some_data` table in BigQuery, only where there's a new table.
```bash
ingestr ingest \
    --source-uri 'postgresql://admin:admin@localhost:8837/web?sslmode=disable' \
    --source-table 'my_schema.some_data' \
    --dest-uri 'bigquery://<your-project-name>?credentials_path=/path/to/service/account.json' \
    --incremental-strategy append
    --incremental-key updated_at
```

### Example

Let's assume you had the following source table:
| id | name | updated_at |
|----|------|------------|
| 1  | John | 2021-01-01 |
| 2  | Jane | 2021-01-01 |

#### First Ingestion
The first time you run the command, it will ingest all the rows into the destination table. Here's how your destination looks like now:

| id | name | updated_at |
|----|------|------------|
| 1  | John | 2021-01-01 |
| 2  | Jane | 2021-01-01 |

#### Second Ingestion, no new data
When there's no new data in the source table, the destination table will remain the same.

#### Third Ingestion, new data
Let's say John changed his name to Johnny, and Jane's `updated_at` was updated to `2021-01-02`, e.g. your source:
| id | name   | updated_at |
|----|--------|------------|
| 1  | Johnny | 2021-01-02 |
| 2  | Jane   | 2021-01-01 |


When you run the command again, it will only ingest the new rows into the destination table. Here's how your destination looks like now:
| id | name   | updated_at |
|----|--------|------------|
| 1  | John   | 2021-01-01 |
| 2  | Jane   | 2021-01-01 |
| 1  | Johnny | 2021-01-02 |

**Notice the last row in the table:** it's the new row that was ingested from the source table.

The behavior is the same if there were new rows in the source table, they would be appended to the destination table if they have `updated_at` that is **later than the latest record** in the destination table.

> [!TIP]
> The `append` strategy allows you to keep a version history of your data, as it will keep appending the new rows to the destination table. You can use it to build [Slowly Changing Dimensions (SCD) Type 2](https://en.wikipedia.org/wiki/Slowly_changing_dimension#Type_2:_add_new_row) tables, for example.


## Merge
Merge will merge the new rows with the existing rows in the destination table, insert the new ones and update the existing ones with the new values. By default, it will merge all the rows. If you'd like to use it as an incremental strategy, you should provide an `incremental_key` as well as a `primary_key` to find the right rows to update.

The following example below will merge the new rows from the `my_schema.some_data` table in Postgres to the `my_schema.some_data` table in BigQuery, only where there's a new table.
```bash
ingestr ingest \
    --source-uri 'postgresql://admin:admin@localhost:8837/web?sslmode=disable' \
    --source-table 'my_schema.some_data' \
    --dest-uri 'bigquery://<your-project-name>?credentials_path=/path/to/service/account.json' \
    --incremental-strategy merge
    --incremental-key updated_at
    --primary-key id
```

Here's how the merge strategy works:
- If the row with the `primary_key` exists in the destination table, it will be updated with the new values from the source table.
- If the row with the `primary_key` doesn't exist in the destination table, it will be inserted into the destination table.
- If the row with the `primary_key` exists in the destination table but not in the source table, it will remain in the destination table.
- If the row with the `primary_key` doesn't exist in the destination table but exists in the source table, it will be inserted into the destination table.

### Example

Let's assume you had the following source table:
| id | name | updated_at |
|----|------|------------|
| 1  | John | 2021-01-01 |
| 2  | Jane | 2021-01-01 |

#### First Ingestion
The first time you run the command, it will ingest all the rows into the destination table. Here's how your destination looks like now:

| id | name | updated_at |
|----|------|------------|
| 1  | John | 2021-01-01 |
| 2  | Jane | 2021-01-01 |

#### Second Ingestion, no new data
When there's no new data in the source table, the destination table will remain the same.

#### Third Ingestion, new data
Let's say John changed his name to Johnny, e.g. your source:
| id | name   | updated_at |
|----|--------|------------|
| 1  | Johnny | 2021-01-02 |
| 2  | Jane   | 2021-01-01 |
    
When you run the command again, it will merge the new rows into the destination table. Here's how your destination looks like now:
| id | name   | updated_at |
|----|--------|------------|
| 1  | Johnny | 2021-01-02 |
| 2  | Jane   | 2021-01-01 |

**Notice the first row in the table:** it's the updated row that was ingested from the source table.

The behavior is the same if there were new rows in the source table, they would be merged into the destination table if they have `updated_at` that is **later than the latest record** in the destination table.

> [!TIP]
> The `merge` strategy is different from the `append` strategy, as it will update the existing rows in the destination table with the new values from the source table. It's useful when you want to keep the latest version of your data in the destination table.

> [!CAUTION]
> For the cases where there's a primary key match, the `merge` strategy will **update** the existing rows in the destination table with the new values from the source table. Use with caution, as it can lead to data loss if not used properly, as well as data processing charges if your data warehouse charges for updates.

## Delete+Insert
Delete+Insert will delete the existing rows in the destination table that match the `incremental_key` and then insert the new rows from the source table. By default, it will delete and insert all the rows. If you'd like to use it as an incremental strategy, you should provide an `incremental_key`.

The following example below will delete the existing rows in the `my_schema.some_data` table in BigQuery that match the `updated_at` and then insert the new rows from the `my_schema.some_data` table in Postgres.
```bash
ingestr ingest \
    --source-uri 'postgresql://admin:admin@localhost:8837/web?sslmode=disable' \
    --source-table 'my_schema.some_data' \
    --dest-uri 'bigquery://<your-project-name>?credentials_path=/path/to/service/account.json' \
    --incremental-strategy delete+insert
    --incremental-key updated_at
```

Here's how the delete+insert strategy works:
- The new rows from the source table will be inserted into a staging table in the destination database.
- The existing rows in the destination table that match the `incremental_key` will be deleted.
- The new rows from the staging table will be inserted into the destination table.

A few important notes about the `delete+insert` strategy: 
- it does not guarantee the order of the rows in the destination table, as it will delete and insert the rows in the destination table.
- it does not deduplicate the rows in the destination table, as it will delete and insert the rows in the destination table, which means you may have multiple rows with the same `incremental_key` in the destination table.

### Example
Let's assume you had the following source table:
| id | name | updated_at |
|----|------|------------|
| 1  | John | 2021-01-01 |
| 2  | Jane | 2021-01-01 |

#### First Ingestion
The first time you run the command, it will ingest all the rows into the destination table. Here's how your destination looks like now:
| id | name | updated_at |
|----|------|------------|
| 1  | John | 2021-01-01 |
| 2  | Jane | 2021-01-01 |

#### Second Ingestion, no new data
Even when there's no new data in the source table, the rows from the source table will be inserted into a staging table in the destination database, and then the existing rows in the destination table that match the `incremental_key` will be deleted, and then the new rows from the staging table will be inserted into the destination table. The destination table will remain the same for the case of this example.
> [!CAUTION]
> If you had rows in the destination table that does not exist in the source table, they will be deleted from the destination table.

#### Third Ingestion, new data
Let's say John changed his name to Johnny, e.g. your source:
| id | name   | updated_at |
|----|--------|------------|
| 1  | Johnny | 2021-01-02 |
| 2  | Jane   | 2021-01-01 |

When you run the command again, it will delete the existing rows in the destination table that match the `incremental_key` and then insert the new rows from the source table. Here's how your destination looks like now:
| id | name   | updated_at |
|----|--------|------------|
| 1  | Johnny | 2021-01-02 |
| 2  | Jane   | 2021-01-01 |

**Notice the first row in the table:** it's the updated row that was ingested from the source table.

The behavior is the same if there were new rows in the source table, they would be deleted and inserted into the destination table if they have `updated_at` that is **later than the latest record** in the destination table.

> [!TIP]
> The `delete+insert` strategy is useful when you want to keep the destination table clean, as it will delete the existing rows in the destination table that match the `incremental_key` and then insert the new rows from the source table. `delete+insert` strategy also allows you to backfill the data, e.g. going back to a past date and ingesting the data again.


## Conclusion
Incremental loading is a powerful feature that allows you to ingest only the new rows from the source table into the destination table. It's useful when you want to keep the destination table up-to-date with the source table, as well as when you want to keep a version history of your data in the destination table. However, there are a few things to keep in mind when using incremental loading:

- If you can and your data is not huge, use the `replace` strategy, as it's the simplest strategy and it will replace the entire destination table with the source table, which will always give you a clean exact replica of the source table.
- If you want to keep a version history of your data, use the `append` strategy, as it will keep appending the new rows to the destination table, which will give you a version history of your data.
- If you want to keep the latest version of your data in the destination table and your table has a natural primary key, such as a user ID, use the `merge` strategy, as it will update the existing rows in the destination table with the new values from the source table.
- If you want to keep the destination table clean and you want to backfill the data, use the `delete+insert` strategy, as it will delete the existing rows in the destination table that match the `incremental_key` and then insert the new rows from the source table.

> [!TIP]
> Even though the document tries to explain, there's no better learning than trying it yourself. You can use the [Quickstart](/getting-started/quickstart.md) to try the incremental loading strategies yourself.