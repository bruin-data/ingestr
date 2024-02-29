# Mongo Database

MongoDB is a NoSQL database that stores JSON-like documents.

Using this `dlt` verified example and
pipeline example, you can load entire databases or specific collections from MongoDB to a
[destination](https://dlthub.com/docs/dlt-ecosystem/destinations/) of your choice. You can load the
following source using this pipeline example:

| Name               | Description                                |
|--------------------|--------------------------------------------|
| mongodb            | loads a specific MongoDB database          |
| mongodb_collection | loads a collection from a MongoDB database |

## Initialize the pipeline

```bash
dlt init mongodb duckdb
```

Here, we chose duckdb as the destination. Alternatively, you can also choose redshift, bigquery, or
any of the other [destinations](https://dlthub.com/docs/dlt-ecosystem/destinations/).

## Setup verified source

To setup MongoDB and grab credentials refer to the
[full documentation here.](https://dlthub.com/docs/dlt-ecosystem/verified-sources/mongodb)

## Add credentials

1. Open `.dlt/secrets.toml`.

1. Add the MongoDB credentials as follows:

   ```toml
   # Put your secret values and credentials here.
   # Note: Do not share this file and do not push it to GitHub!
   connection_url = "mongodb://dbuser:passwd@host.or.ip:27017" # Database connection URL.
   ```

1. Update ".dlt/config.toml" with database and collection names:

   ```
   [your_pipeline_name]  # Set your pipeline name!
   database = "defaultDB"  # Database name (Optional), default database is loaded if not provided.
   collection_names = ["collection_1", "collection_2"] # Collection names (Optional), All collections are loaded if not provided.
   ```

   > Optionally, you can set database and collection names in ".dlt/secrets.toml" under [sources.mongodb] without listing the pipeline name.

1. Enter credentials for your chosen destination as per the
   [docs.](https://dlthub.com/docs/dlt-ecosystem/destinations/)

## Run the pipeline example

1. Install the necessary dependencies by running the following command:

   ```bash
   pip install -r requirements.txt
   ```

1. Now the pipeline can be run by using the command:

   ```bash
   python mongodb_pipeline.py
   ```

1. To make sure that everything is loaded as expected, use the command:

   ```bash
   dlt pipeline <pipeline_name> show
   ```

   For example, the pipeline_name for the above pipeline example is `local_mongo`, you may also use
   any custom name instead.

💡 To explore additional customizations for this pipeline, we recommend referring to the official dlt
MongoDB verified documentation. It provides comprehensive information and guidance on how to further
customize and tailor the pipeline to suit your specific needs. You can find the dlt MongoDB
documentation in
[Setup Guide: MongoDB.](https://dlthub.com/docs/dlt-ecosystem/verified-sources/mongodb)
