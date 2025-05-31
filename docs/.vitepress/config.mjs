import { defineConfig } from "vitepress";

// https://vitepress.dev/reference/site-config
export default defineConfig({
  title: "ingestr",
  description: "Ingest & copy data between any source and any destination",
  base: "/ingestr/",
  head: [
    [
      "script",
      {
        async: "",
        src: "https://www.googletagmanager.com/gtag/js?id=G-MZJ20PP4MJ",
      },
    ],
    [
      "script",
      {},
      `window.dataLayer = window.dataLayer || [];
      function gtag(){dataLayer.push(arguments);}
      gtag('js', new Date());
      gtag('config', 'G-MZJ20PP4MJ');`,
    ],
  ],
  themeConfig: {
    // https://vitepress.dev/reference/default-theme-config
    nav: [
      { text: "Home", link: "/" },
      { text: "Getting started", link: "/getting-started/quickstart.md" },
    ],
    outline: "deep",
    search: {
      provider: 'local'
    },

    sidebar: [
      {
        text: "Getting started",
        items: [
          { text: "Quickstart", link: "/getting-started/quickstart.md" },
          { text: "Core Concepts", link: "/getting-started/core-concepts.md" },
          {
            text: "Incremental Loading",
            link: "/getting-started/incremental-loading.md",
          },
          { text: "Telemetry", link: "/getting-started/telemetry.md" },
        ],
      },
      {
        text: "Commands",
        items: [
          { text: "ingest", link: "/commands/ingest.md" },
          { text: "example-uris", link: "/commands/example-uris.md" },
        ],
      },
      {
        text: "Tutorials",
        items: [
          { text: "Load Kinesis Data to BigQuery", link: "/tutorials/load-kinesis-bigquery.md" },
          { text: "Load Personio Data to DuckDB", link: "/tutorials/load-personio-duckdb.md" },
          { text: "Load Stripe Data to Postgres", link: "/tutorials/load-stripe-postgres.md" },
        ],
      },
      {
        text: "Sources & Destinations",
        items: [
          {
            text: "Databases",
            collapsed: false,
            items: [
              { text: "AWS Athena", link: "/supported-sources/athena.md" },
              { text: "AWS Redshift", link: "/supported-sources/redshift.md" },
              { text: "ClickHouse", link: "/supported-sources/clickhouse.md" },
              { text: "Databricks", link: "/supported-sources/databricks.md" },
              { text: "DuckDB", link: "/supported-sources/duckdb.md" },
              { text: "DynamoDB", link: "/supported-sources/dynamodb.md" },
              { text: "Elasticsearch", link: "/supported-sources/elasticsearch.md" },
              {
                text: "Google BigQuery",
                link: "/supported-sources/bigquery.md",
              },
              { text: "GCP Spanner", link: "/supported-sources/spanner.md" },
              { text: "IBM Db2", link: "/supported-sources/db2.md" },
              { text: "Kafka", link: "/supported-sources/kafka.md" },
              { text: "Local CSV Files", link: "/supported-sources/csv.md" },
              {
                text: "Microsoft SQL Server",
                link: "/supported-sources/mssql.md",
              },
              { text: "MongoDB", link: "/supported-sources/mongodb.md" },
              { text: "MySQL", link: "/supported-sources/mysql.md" },
              { text: "Oracle", link: "/supported-sources/oracle.md" },
              { text: "Postgres", link: "/supported-sources/postgres.md" },
              { text: "SAP Hana", link: "/supported-sources/sap-hana.md" },
              { text: "Snowflake", link: "/supported-sources/snowflake.md" },
              { text: "SQLite", link: "/supported-sources/sqlite.md" },
              {
                text: "Experimental",
                items: [
                  { text: "Custom Queries", link: "/supported-sources/custom_queries.md" },
                ],
              },
            ],
          },

          {
            text: "Platforms",
            collapsed: false,
            items: [
              { text: "Adjust", link: "/supported-sources/adjust.md" },
              { text: "Airtable", link: "/supported-sources/airtable.md" },
              { text: "Amazon Kinesis", link: "/supported-sources/kinesis.md" },
              { text: "AppsFlyer", link: "/supported-sources/appsflyer.md" },
              { text: "Apple App Store", link: "/supported-sources/appstore.md"},
              { text: "Applovin", link: "/supported-sources/applovin.md"},
              { text: "Applovin Max", link: "/supported-sources/applovin_max.md"},
              { text: "Asana", link: "/supported-sources/asana.md" },
              { text: "Attio", link: "/supported-sources/attio.md" },
              { text: "Chess.com", link: "/supported-sources/chess.md" },
              {
                text: "Facebook Ads",
                link: "/supported-sources/facebook-ads.md",
              },
              { text: "Frankfurter", link: "/supported-sources/frankfurter.md" },
              { text: "Freshdesk", link: "/supported-sources/freshdesk.md" },
              { text: "Google Cloud Storage (GCS)", link: "/supported-sources/gcs.md" },
              { text: "Google Analytics", link: "/supported-sources/google_analytics.md" },
              { text: "Google Ads", link: "/supported-sources/google-ads.md" },
              { text: "GitHub", link: "/supported-sources/github.md" },
              { text: "Google Sheets", link: "/supported-sources/gsheets.md" },
              { text: "Gorgias", link: "/supported-sources/gorgias.md" },
              { text: "HubSpot", link: "/supported-sources/hubspot.md" },
              { text: "Klaviyo", link: "/supported-sources/klaviyo.md" },
              { text: "LinkedIn Ads", link: "/supported-sources/linkedin_ads.md" },
              { text: "Notion", link: "/supported-sources/notion.md" },
              { text: "Personio", link: "/supported-sources/personio.md" },
              { text: "PhantomBuster", link: "/supported-sources/phantombuster.md" },
              { text: "Pipedrive", link: "/supported-sources/pipedrive.md" },
              { text: "S3", link: "/supported-sources/s3.md" },
              { text: "Salesforce", link: "/supported-sources/salesforce.md" },
              { text: "Shopify", link: "/supported-sources/shopify.md" },
              { text: "Slack", link: "/supported-sources/slack.md" },
              { text: "Smartsheet", link: "/supported-sources/smartsheets.md" },
              { text: "Solidgate", link: "/supported-sources/solidgate.md" },
              { text: "Stripe", link: "/supported-sources/stripe.md" },
              { text: "TikTok Ads", link: "/supported-sources/tiktok-ads.md" },
              { text: "Zendesk", link: "/supported-sources/zendesk.md" },
            ],
          },
        ],
      },
    ],

    socialLinks: [
      { icon: "github", link: "https://github.com/bruin-data/ingestr" },
    ],
  },
});
