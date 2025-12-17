import { defineConfig } from "vitepress";

// https://vitepress.dev/reference/site-config
export default defineConfig({
  title: "ingestr",
  description: "Ingest & copy data between any source and any destination",
  base: "/ingestr/",
  sitemap: {
    hostname: 'https://getbruin.com',
    transformItems: (items) => {
      return items.map((item) => {
        const cleaned = item.url.replace(/^\/ingestr\//, '');
        item.url = `https://getbruin.com/docs/ingestr/${cleaned}`;
        return item;
      });
    },
    trailingSlash: true,
  },
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
      { text: "Introduction", link: "/" },
      { text: "Quickstart", link: "/getting-started/quickstart.md" },
    ],
    outline: "deep",
    search: {
      provider: 'local'
    },

    sidebar: [
      {
        text: "Introduction",
        link: "/",
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
              { text: "Adjust", link: "/supported-sources/adjust" },
              { text: "Airtable", link: "/supported-sources/airtable" },
              { text: "Amazon Kinesis", link: "/supported-sources/kinesis" },
              { text: "Anthropic", link: "/supported-sources/anthropic" },
              { text: "AppsFlyer", link: "/supported-sources/appsflyer" },
              { text: "Apple App Store", link: "/supported-sources/appstore"},
              { text: "Applovin", link: "/supported-sources/applovin"},
              { text: "Applovin Max", link: "/supported-sources/applovin_max"},
              { text: "Asana", link: "/supported-sources/asana" },
              { text: "Attio", link: "/supported-sources/attio" },
              { text: "Bruin", link: "/supported-sources/bruin" },
              { text: "Chess.com", link: "/supported-sources/chess" },
              { text: "ClickUp", link: "/supported-sources/clickup" },
              { text: "Cursor", link: "/supported-sources/cursor" },
              { text: "Docebo", link: "/supported-sources/docebo" },
              {
                text: "Facebook Ads",
                link: "/supported-sources/facebook-ads",
              },
              { text: "Fluxx", link: "/supported-sources/fluxx" },
              { text: "Frankfurter", link: "/supported-sources/frankfurter" },
              { text: "Freshdesk", link: "/supported-sources/freshdesk" },
              { text: "FundraiseUp", link: "/supported-sources/fundraiseup" },
              { text: "Trustpilot", link: "/supported-sources/trustpilot" },
              { text: "Google Cloud Storage (GCS)", link: "/supported-sources/gcs" },
              { text: "Google Analytics", link: "/supported-sources/google_analytics" },
              { text: "Google Ads", link: "/supported-sources/google-ads" },
              { text: "GitHub", link: "/supported-sources/github" },
              { text: "Google Sheets", link: "/supported-sources/gsheets" },
              { text: "Hostaway", link: "/supported-sources/hostaway" },
              { text: "Gorgias", link: "/supported-sources/gorgias" },
              { text: "HubSpot", link: "/supported-sources/hubspot" },
              { text: "Internet Society Pulse", link: "/supported-sources/isoc-pulse" },
              { text: "Jira", link: "/supported-sources/jira" },
              { text: "Klaviyo", link: "/supported-sources/klaviyo" },
              { text: "Linear", link: "/supported-sources/linear" },
              { text: "LinkedIn Ads", link: "/supported-sources/linkedin_ads" },
              { text: "Mailchimp", link: "/supported-sources/mailchimp" },
              { text: "Mixpanel", link: "/supported-sources/mixpanel" },
              { text: "Monday", link: "/supported-sources/monday" },
              { text: "Notion", link: "/supported-sources/notion" },
              { text: "Personio", link: "/supported-sources/personio" },
              { text: "PhantomBuster", link: "/supported-sources/phantombuster" },
              { text: "Pinterest", link: "/supported-sources/pinterest" },
              { text: "Pipedrive", link: "/supported-sources/pipedrive" },
              { text: "Primer", link: "/supported-sources/primer" },
              { text: "Plus Vibe AI", link: "/supported-sources/plusvibeai" },
              { text: "QuickBooks", link: "/supported-sources/quickbooks" },
              { text: "RevenueCat", link: "/supported-sources/revenuecat" },
              { text: "S3", link: "/supported-sources/s3" },
              { text: "Salesforce", link: "/supported-sources/salesforce" },
              { text: "SFTP", link: "/supported-sources/sftp"},
              { text: "Shopify", link: "/supported-sources/shopify" },
              { text: "Slack", link: "/supported-sources/slack" },
              { text: "Smartsheet", link: "/supported-sources/smartsheets" },
              { text: "Snapchat Ads", link: "/supported-sources/snapchat-ads" },
              { text: "Solidgate", link: "/supported-sources/solidgate" },
              { text: "Stripe", link: "/supported-sources/stripe" },
              { text: "TikTok Ads", link: "/supported-sources/tiktok-ads" },
              { text: "Wise", link: "/supported-sources/wise" },
              { text: "Zendesk", link: "/supported-sources/zendesk" },
              { text: "Zoom", link: "/supported-sources/zoom" },
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
