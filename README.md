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

## Installation
We recommend using [uv](https://github.com/astral-sh/uv) to run `ingestr`.

```
pip install uv
uvx ingestr
```

Alternatively, if you'd like to install it globally:
```
uv pip install --system ingestr
```

While installation with vanilla `pip` is possible, it's an order of magnitude slower.

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
        <td>BigQuery</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>ClickHouse</td>
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
        <td>-</td>
    </tr>
    <tr>
        <td>Local CSV file</td>
        <td>✅</td>
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
        <td>❌</td>
    </tr>
    <tr>
        <td>MySQL</td>
        <td>✅</td>
        <td>❌</td>
    </tr>
    <tr>
        <td>Oracle</td>
        <td>✅</td>
        <td>❌</td>
    </tr>
    <tr>
        <td>Postgres</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
     <tr>
        <td>Redshift</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>SAP Hana</td>
        <td>✅</td>
        <td>❌</td>
    </tr>
    <tr>
        <td>Snowflake</td>
        <td>✅</td>
        <td>✅</td>
    </tr>
    <tr>
        <td>SQLite</td>
        <td>✅</td>
        <td>❌</td>
    </tr>
    <tr>
        <td colspan="3" style='text-align:center;'><strong>Platforms</strong></td>
    </tr>
        <td>Adjust</td>
        <td>✅</td>
        <td>-</td>
    <tr>
        <td>Airtable</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Amazon Kinesis</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Apache Kafka</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>AppsFlyer</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>App Store</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Asana</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Chess.com</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Facebook Ads</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Github</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Gorgias</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Google Sheets</td>
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
        <td>Klaviyo</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>LinkedIn Ads</td>
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
        <td>S3</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Salesforce</td>
        <td>✅</td>
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
        <td>Stripe</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>TikTok Ads</td>
        <td>✅</td>
        <td>-</td>
    </tr>
    <tr>
        <td>Zendesk</td>
        <td>✅</td>
        <td>-</td>
    </tr>
</table>

Feel free to create an issue if you'd like to see support for another source or destination.

## Acknowledgements

This project would not have been possible without the amazing work done by the [SQLAlchemy](https://www.sqlalchemy.org/) and [dlt](https://dlthub.com/) teams. We relied on their work to connect to various sources and destinations, and built `ingestr` as a simple, opinionated wrapper around their work.
