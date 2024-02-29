import { defineConfig } from "vitepress";

// https://vitepress.dev/reference/site-config
export default defineConfig({
  title: "ingestr",
  description: "Ingest & copy data between any source and any destination",
  base: '/ingestr/',
  head: [
    [
      'script',
      { async: '', src: 'https://www.googletagmanager.com/gtag/js?id=G-MZJ20PP4MJ' }
    ],
    [
      'script',
      {},
      `window.dataLayer = window.dataLayer || [];
      function gtag(){dataLayer.push(arguments);}
      gtag('js', new Date());
      gtag('config', 'G-MZJ20PP4MJ');`
    ]
  ],
  themeConfig: {
    // https://vitepress.dev/reference/default-theme-config
    nav: [
      { text: "Home", link: "/" },
      { text: "Getting started", link: "/getting-started/quickstart.md" },
    ],

    sidebar: [
      {
        text: "Getting started",
        items: [
          { text: "Quickstart", link: "/getting-started/quickstart.md" },
          { text: "Core Concepts", link: "/getting-started/core-concepts.md" },
          { text: "Incremental Loading", link: "/getting-started/incremental-loading.md" },
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
          { text: "AWS Redshift", link: "/supported-sources/redshift.md" },
          { text: "Databricks", link: "/supported-sources/databricks.md" },
          { text: "DuckDB", link: "/supported-sources/duckdb.md" },
          { text: "Google BigQuery", link: "/supported-sources/bigquery.md" },
          { text: "Local CSV Files", link: "/supported-sources/csv.md" },
          { text: "Microsoft SQL Server", link: "/supported-sources/mssql.md" },
          { text: "MongoDB", link: "/supported-sources/mongodb.md" },
          { text: "MySQL", link: "/supported-sources/mysql.md" },
          { text: "Oracle", link: "/supported-sources/oracle.md" },
          { text: "Overview", link: "/supported-sources/overview.md" },
          { text: "Postgres", link: "/supported-sources/postgres.md" },
          { text: "Snowflake", link: "/supported-sources/snowflake.md" },
          { text: "SQLite", link: "/supported-sources/sqlite.md" },
        ],
      },
    ],

    socialLinks: [{ icon: "github", link: "https://github.com/bruin-data/ingestr" }],
  },
});
