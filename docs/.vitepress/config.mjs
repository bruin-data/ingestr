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
        text: "Sources & Destinations",
        items: [
          {
            text: "Databases",
            collapsed: false,
            items: [
              { text: "AWS Redshift", link: "/supported-sources/redshift.md" },
              { text: "Databricks", link: "/supported-sources/databricks.md" },
              { text: "DuckDB", link: "/supported-sources/duckdb.md" },
              {
                text: "Google BigQuery",
                link: "/supported-sources/bigquery.md",
              },
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
            ],
          },

          {
            text: "Platforms",
            collapsed: false,
            items: [
              { text: "Adjust", link: "/supported-sources/adjust.md" },
              { text: "Airtable", link: "/supported-sources/airtable.md" },
              { text: "AppsFlyer", link: "/supported-sources/appsflyer.md" },
              { text: "Chess.com", link: "/supported-sources/chess.md" },
              {
                text: "Facebook Ads",
                link: "/supported-sources/facebook-ads.md",
              },
              { text: "Google Sheets", link: "/supported-sources/gsheets.md" },
              { text: "Gorgias", link: "/supported-sources/gorgias.md" },
              { text: "HubSpot", link: "/supported-sources/hubspot.md" },
              { text: "Klaviyo", link: "/supported-sources/klaviyo.md" },
              { text: "Notion", link: "/supported-sources/notion.md" },
              { text: "S3", link: "/supported-sources/s3.md" },
              { text: "Shopify", link: "/supported-sources/shopify.md" },
              { text: "Slack", link: "/supported-sources/slack.md" },
              { text: "Stripe", link: "/supported-sources/stripe.md" },
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
