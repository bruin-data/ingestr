# Amazon Kinesis

[Amazon Kinesis](https://docs.aws.amazon.com/streams/latest/dev/key-concepts.html) is a
cloud-based service for real-time data streaming and analytics, enabling the processing and analysis
of large streams of data in real time.

Resources that can be loaded using this verified source are:

| Name             | Description                                                                              |
|------------------|------------------------------------------------------------------------------------------|
| kinesis_stream   | Load messages from the specified stream                                                  |

## Initialize the pipeline

```bash
dlt init kinesis duckdb
```

Here, we chose `duckdb` as the destination. Alternatively, you can also choose `redshift`,
`bigquery`, or any of the other [destinations](https://dlthub.com/docs/dlt-ecosystem/destinations/).

## Setup verified source

To grab Kinesis credentials and configure the verified source, please refer to the
[full documentation here.](https://dlthub.com/docs/dlt-ecosystem/verified-sources/amazon_kinesis#grab-credentials)

