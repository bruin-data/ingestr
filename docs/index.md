---
# https://vitepress.dev/reference/default-theme-home-page
layout: home

hero:
  name: "ingestr"
  text: Copy data between any source and any destination
  tagline: "ingestr is a command-line application that allows copying data from any source into any destination database."
  image:
    src: https://github.com/bruin-data/ingestr/blob/main/resources/demo.gif?raw=true
    alt: ingestr logo
  actions:
    - theme: brand
      text: Getting Started
      link: /getting-started/quickstart.md
    - theme: alt
      text: GitHub
      link: https://github.com/bruin-data/ingestr

features:
  - title: Single command
    details: ingestr allows copying & ingesting data from any source to any destination with a single command.
  - title: Many sources & destinations
    details: ingestr supports all common source and destination databases.
  - title: Incremental Loading
    details: ingestr supports both full-refresh as well as incremental loading modes.
---
<div style="margin-top: 12px; line-height: 2em; text-align: center;">

<Badge type="info" text="Postgres" /> <Badge type="danger" text="BigQuery" /> <Badge type="tip" text="Snowflake" /> <Badge type="warning" text="Redshift" /> <Badge type="info" text="Databricks" /> <Badge type="danger" text="DuckDB" /> <Badge type="tip" text="Microsoft SQL Server" /> <Badge type="warning" text="Local CSV file" /> <Badge type="info" text="MongoDB" /> <Badge type="danger" text="Oracle" /> <Badge type="tip" text="SAP Hana" /> <Badge type="warning" text="SQLite" /> <Badge type="info" text="MySQL" /> <Badge type="danger" text="Google Sheets" /> <Badge type="tip" text="Notion" /> <Badge type="warning" text="Shopify" />
</div>
