<div align="center">
    <img src="https://github.com/bruin-data/ingestr/blob/main/resources/ingestr.svg?raw=true" width="500" />
    <p>Copy data from any source to any destination without any code</p>
    <img src="https://github.com/bruin-data/ingestr/blob/main/resources/demo.gif?raw=true" width="750" />
</div>

<div align="center" style="margin-top: 24px;">
  <a target="_blank" href="https://join.slack.com/t/bruindatacommunity/shared_invite/zt-2dl2i8foy-bVsuMUauHeN9M2laVm3ZVg" style="background:none">
    <img src="https://img.shields.io/badge/slack-join-dlt.svg?color=d95f5f&logo=slack" style="width: 180px;"  />
  </a>
</div>

---

ingestr is a command-line app that allows you to ingest data from any source into any destination using simple command-line flags, no code necessary.

- ✨ copy data from your database into any destination
- ➕ incremental loading: `append`, `merge` or `delete+insert`
- 🐍 single-command installation

ingestr takes away the complexity of managing any backend or writing any code for ingesting data, simply run the command and watch the data land on its destination.

![MongoDB to Postgres benchmark](resources/mongodb-postgres-benchmark.png?raw=true)

## Installation

You can install `ingestr` using the install script:

```sh
curl -LsSf https://getbruin.com/install/ingestr | sh
```

Alternatively, you can install it with pip:

```sh
pip install ingestr
```


## Quickstart

```bash
ingestr ingest \
    --source-uri 'postgresql://admin:admin@localhost:8837/web?sslmode=disable' \
    --source-table 'public.some_data' \
    --dest-uri 'bigquery://<your-project-name>?credentials_path=/path/to/service/account.json' \
    --dest-table 'ingestr.some_data'
```

That's it.

This command:

- gets the table `public.some_data` from the Postgres instance.
- uploads this data to your BigQuery warehouse under the schema `ingestr` and table `some_data`.

## Documentation

You can see the full documentation [here](https://bruin-data.github.io/ingestr/getting-started/quickstart.html).

## Community

Join our Slack community [here](https://join.slack.com/t/bruindatacommunity/shared_invite/zt-2dl2i8foy-bVsuMUauHeN9M2laVm3ZVg).

## Contributing

Pull requests are welcome. However, please open an issue first to discuss what you would like to change. We maybe able to offer you help and feedback regarding any changes you would like to make.

> [!NOTE]
> After cloning `ingestr` make sure to run `make setup` to install githooks.

## Supported sources & destinations
<table>
    <tr>
        <th></th>
        <th>Source</th>
        <th>Destination</th>
    </tr>
    <tr>
        <td colspan="3" style='text-align:center;'><strong>Databases</strong></td>
    </tr>
    <tr>
        <td>AWS Athena</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>AWS Redshift</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>Cassandra</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>ClickHouse</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>Couchbase</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>CrateDB</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>Databricks</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>DuckDB</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>DynamoDB</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>Elasticsearch</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>Google BigQuery</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>GCP Spanner</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>IBM Db2</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>InfluxDB</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Kafka</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Local CSV file</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>Microsoft Fabric</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>Microsoft OneLake</td>
        <td>-</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>Microsoft SQL Server</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>MongoDB</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>MotherDuck</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>MySQL</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>Oracle</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Postgres</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>RabbitMQ</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>SAP Hana</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Snowflake</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>Socrata</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>SQLite</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>Synapse</td>
        <td>-</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>Trino</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td colspan="3" style='text-align:center;'><strong>Platforms</strong></td>
    </tr>
    <tr>
        <td>Adjust</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Airtable</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Allium</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Amazon Kinesis</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Anthropic</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>AppsFlyer</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Apple Ads</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Apple App Store</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Applovin</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Applovin Max</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Asana</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Attio</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Azure Data Lake Storage Gen2</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>Bruin</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Chess.com</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>ClickUp</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Cursor</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Docebo</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Dune</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Facebook Ads</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Fireflies</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Fluxx</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Frankfurter</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Freshdesk</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>FundraiseUp</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>G2</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>GitHub</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Google Ads</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Google Analytics</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Google Cloud Storage (GCS)</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>Google Sheets</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Gorgias</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Granola</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Hostaway</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>HubSpot</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Indeed</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Intercom</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Internet Society Pulse</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Jira</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>JobTread</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Klaviyo</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Linear</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>LinkedIn Ads</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Mailchimp</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Mixpanel</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Monday</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Notion</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Personio</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>PhantomBuster</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Pinterest</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Pipedrive</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Plus Vibe AI</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>PostHog</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Primer</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>QuickBooks</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Reddit Ads</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>RevenueCat</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>S3</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>Salesforce</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>SFTP</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>SendGrid</td>
        <td>Manual testing</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Shopify</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Slack</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Smartsheet</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Snapchat Ads</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Solidgate</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Stripe</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>SurveyMonkey</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>TikTok Ads</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Trustpilot</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Wise</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Zendesk</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Zoom</td>
        <td>✅</td>
        <td>-</td>
    </tr>
</table>

Feel free to create an issue if you'd like to see support for another source or destination.

## License

ingestr is source-available under the [Functional Source License 1.1](https://fsl.software/), with Apache 2.0 as the future license. You can use ingestr freely for internal production use, development, testing, education, research, and professional services. You cannot use ingestr to offer a competing commercial ingestion, ELT, connector, or managed data pipeline product/service.

Each version becomes Apache 2.0 two years after release.
