import { defineConfig } from "vitepress";

// https://vitepress.dev/reference/site-config
export default defineConfig({
  title: "ingestr",
  description: "Ingest & copy data between any source and any destination",
  base: '/ingestr/',
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
          { text: "Overview", link: "/supported-sources/overview.md" },
          { text: "Postgres", link: "/supported-sources/postgres.md" },
          { text: "Google BigQuery", link: "/supported-sources/bigquery.md" },
          { text: "Snowflake", link: "/supported-sources/snowflake.md" },
          { text: "AWS Redshift", link: "/supported-sources/redshift.md" },
          { text: "Databricks", link: "/supported-sources/databricks.md" },
          { text: "DuckDB", link: "/supported-sources/duckdb.md" },
          { text: "Microsoft SQL Server", link: "/supported-sources/mssql.md" },
          { text: "SQLite", link: "/supported-sources/sqlite.md" },
          { text: "MySQL", link: "/supported-sources/mysql.md" },
        ],
      },
    ],

    socialLinks: [{ icon: "github", link: "https://github.com/bruin-data/ingestr" }],
  },
});
