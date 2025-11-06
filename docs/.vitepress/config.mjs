import { defineConfig } from "vitepress";

// https://vitepress.dev/reference/site-config
export default defineConfig({
  title: "ingestr",
  description: "Ingest & copy data between any source and any destination",
  base: "/ingestr/",
  transformPageData(pageData) {
    const canonicalUrl = `https://getbruin.com/docs/ingestr/${pageData.relativePath}`
      .replace(/index\.md$/, '')
      .replace(/\.md$/, '.html')

    pageData.frontmatter.head ??= []
    pageData.frontmatter.head.push([
      'link',
      { rel: 'canonical', href: canonicalUrl }
    ])
  },
  head: [
    [
      "script",
      {},
      `(function(w,d,s,l,i){w[l]=w[l]||[];w[l].push({'gtm.start':
new Date().getTime(),event:'gtm.js'});var f=d.getElementsByTagName(s)[0],
j=d.createElement(s),dl=l!='dataLayer'?'&l='+l:'';j.async=true;j.src=
'https://www.googletagmanager.com/gtm.js?id='+i+dl;f.parentNode.insertBefore(j,f);
})(window,document,'script','dataLayer','GTM-K2L7S5FP');`,
    ],
    [
      "noscript",
      {},
      `<iframe src="https://www.googletagmanager.com/ns.html?id=GTM-K2L7S5FP" height="0" width="0" style="display:none;visibility:hidden"></iframe>`,
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
          { text: "Data Masking", link: "/getting-started/data-masking.md" },
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
              { text: "Couchbase", link: "/supported-sources/couchbase.md" },
              { text: "CrateDB", link: "/supported-sources/cratedb.md" },
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
              { text: "InfluxDB", link: "/supported-sources/influxdb.md" },
              { text: "Kafka", link: "/supported-sources/kafka.md" },
              { text: "Local CSV Files", link: "/supported-sources/csv.md" },
              {
                text: "Microsoft SQL Server",
                link: "/supported-sources/mssql.md",
              },
              { text: "MongoDB", link: "/supported-sources/mongodb.md" },
              { text: "MotherDuck", link: "/supported-sources/motherduck.md" },
              { text: "MySQL", link: "/supported-sources/mysql.md" },
              { text: "Oracle", link: "/supported-sources/oracle.md" },
              { text: "Postgres", link: "/supported-sources/postgres.md" },
              { text: "SAP Hana", link: "/supported-sources/sap-hana.md" },
              { text: "Snowflake", link: "/supported-sources/snowflake.md" },
              { text: "Socrata", link: "/supported-sources/socrata.md" },
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
              { text: "Anthropic", link: "/supported-sources/anthropic.md" },
              { text: "AppsFlyer", link: "/supported-sources/appsflyer.md" },
              { text: "Apple App Store", link: "/supported-sources/appstore.md"},
              { text: "Applovin", link: "/supported-sources/applovin.md"},
              { text: "Applovin Max", link: "/supported-sources/applovin_max.md"},
              { text: "Asana", link: "/supported-sources/asana.md" },
              { text: "Attio", link: "/supported-sources/attio.md" },
              { text: "Chess.com", link: "/supported-sources/chess.md" },
              { text: "ClickUp", link: "/supported-sources/clickup.md" },
              { text: "Cursor", link: "/supported-sources/cursor.md" },
              { text: "Docebo", link: "/supported-sources/docebo.md" },
              {
                text: "Facebook Ads",
                link: "/supported-sources/facebook-ads.md",
              },
              { text: "Fluxx", link: "/supported-sources/fluxx.md" },
              { text: "Frankfurter", link: "/supported-sources/frankfurter.md" },
              { text: "Freshdesk", link: "/supported-sources/freshdesk.md" },
              { text: "FundraiseUp", link: "/supported-sources/fundraiseup.md" },
              { text: "Trustpilot", link: "/supported-sources/trustpilot.md" },
              { text: "Google Cloud Storage (GCS)", link: "/supported-sources/gcs.md" },
              { text: "Google Analytics", link: "/supported-sources/google_analytics.md" },
              { text: "Google Ads", link: "/supported-sources/google-ads.md" },
              { text: "GitHub", link: "/supported-sources/github.md" },
              { text: "Google Sheets", link: "/supported-sources/gsheets.md" },
              { text: "Hostaway", link: "/supported-sources/hostaway.md" },
              { text: "Gorgias", link: "/supported-sources/gorgias.md" },
              { text: "HubSpot", link: "/supported-sources/hubspot.md" },
              { text: "Internet Society Pulse", link: "/supported-sources/isoc-pulse.md" },
              { text: "Jira", link: "/supported-sources/jira.md" },
              { text: "Klaviyo", link: "/supported-sources/klaviyo.md" },
              { text: "Linear", link: "/supported-sources/linear.md" },
              { text: "LinkedIn Ads", link: "/supported-sources/linkedin_ads.md" },
              { text: "Mailchimp", link: "/supported-sources/mailchimp.md" },
              { text: "Mixpanel", link: "/supported-sources/mixpanel.md" },
              { text: "Monday", link: "/supported-sources/monday.md" },
              { text: "Notion", link: "/supported-sources/notion.md" },
              { text: "Personio", link: "/supported-sources/personio.md" },
              { text: "PhantomBuster", link: "/supported-sources/phantombuster.md" },
              { text: "Pinterest", link: "/supported-sources/pinterest.md" },
              { text: "Pipedrive", link: "/supported-sources/pipedrive.md" },
              { text: "Plus Vibe AI", link: "/supported-sources/plusvibeai.md" },
              { text: "QuickBooks", link: "/supported-sources/quickbooks.md" },
              { text: "RevenueCat", link: "/supported-sources/revenuecat.md" },
              { text: "S3", link: "/supported-sources/s3.md" },
              { text: "Salesforce", link: "/supported-sources/salesforce.md" },
              { text: "SFTP", link: "/supported-sources/sftp.md"},
              { text: "Shopify", link: "/supported-sources/shopify.md" },
              { text: "Slack", link: "/supported-sources/slack.md" },
              { text: "Smartsheet", link: "/supported-sources/smartsheets.md" },
              { text: "Solidgate", link: "/supported-sources/solidgate.md" },
              { text: "Stripe", link: "/supported-sources/stripe.md" },
              { text: "TikTok Ads", link: "/supported-sources/tiktok-ads.md" },
              { text: "Wise", link: "/supported-sources/wise.md" },
              { text: "Zendesk", link: "/supported-sources/zendesk.md" },
              { text: "Zoom", link: "/supported-sources/zoom.md" },
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
