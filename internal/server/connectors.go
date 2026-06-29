package server

import (
	"net/url"
	"strings"
)

type ConnectorField struct {
	Name        string        `json:"name"`
	Label       string        `json:"label"`
	Type        string        `json:"type"` // "string", "password", "number", "file", "select", "checkbox"
	Required    bool          `json:"required"`
	Default     string        `json:"default,omitempty"`
	Placeholder string        `json:"placeholder,omitempty"`
	Options     []FieldOption `json:"options,omitempty"`
}

type FieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type ConnectorType struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Schemes       []string         `json:"schemes"`
	IsSource      bool             `json:"isSource"`
	IsDestination bool             `json:"isDestination"`
	Fields        []ConnectorField `json:"fields"`
}

func GetConnectors() []ConnectorType {
	connectors := []ConnectorType{
		{
			ID:            "postgres",
			Name:          "PostgreSQL",
			Schemes:       []string{"postgres", "postgresql", "postgresql+psycopg2"},
			IsSource:      true,
			IsDestination: true,
			Fields: []ConnectorField{
				{Name: "host", Label: "Host", Type: "string", Required: true, Placeholder: "localhost"},
				{Name: "port", Label: "Port", Type: "number", Required: false, Default: "5432", Placeholder: "5432"},
				{Name: "user", Label: "Username", Type: "string", Required: true, Placeholder: "postgres"},
				{Name: "password", Label: "Password", Type: "password", Required: false},
				{Name: "database", Label: "Database", Type: "string", Required: true, Placeholder: "mydb"},
				{Name: "sslmode", Label: "SSL Mode", Type: "select", Required: false, Default: "prefer", Options: []FieldOption{
					{Value: "disable", Label: "Disable"},
					{Value: "prefer", Label: "Prefer"},
					{Value: "require", Label: "Require"},
					{Value: "verify-ca", Label: "Verify CA"},
					{Value: "verify-full", Label: "Verify Full"},
				}},
			},
		},
		{
			ID:            "mysql",
			Name:          "MySQL",
			Schemes:       []string{"mysql", "mysql+pymysql", "mariadb"},
			IsSource:      true,
			IsDestination: true,
			Fields: []ConnectorField{
				{Name: "host", Label: "Host", Type: "string", Required: true, Placeholder: "localhost"},
				{Name: "port", Label: "Port", Type: "number", Required: false, Default: "3306", Placeholder: "3306"},
				{Name: "user", Label: "Username", Type: "string", Required: true, Placeholder: "root"},
				{Name: "password", Label: "Password", Type: "password", Required: false},
				{Name: "database", Label: "Database", Type: "string", Required: true, Placeholder: "mydb"},
			},
		},
		{
			ID:            "mssql",
			Name:          "SQL Server",
			Schemes:       []string{"mssql", "sqlserver", "mssql+pyodbc"},
			IsSource:      true,
			IsDestination: true,
			Fields: []ConnectorField{
				{Name: "host", Label: "Host", Type: "string", Required: true, Placeholder: "localhost"},
				{Name: "port", Label: "Port", Type: "number", Required: false, Default: "1433", Placeholder: "1433"},
				{Name: "user", Label: "Username", Type: "string", Required: true, Placeholder: "sa"},
				{Name: "password", Label: "Password", Type: "password", Required: false},
				{Name: "database", Label: "Database", Type: "string", Required: true, Placeholder: "master"},
			},
		},
		genericURIConnector("postgres-cdc", "PostgreSQL CDC", []string{"postgres+cdc", "postgresql+cdc"}, true, false),
		genericURIConnector("mysql-cdc", "MySQL CDC", []string{"mysql+cdc", "mysql+pymysql+cdc", "mariadb+cdc"}, true, false),
		genericURIConnector("mssql-cdc", "SQL Server CDC", []string{"mssql+cdc", "sqlserver+cdc", "azuresql+cdc", "azure-sql+cdc"}, true, false),
		genericURIConnector("mssql-ct", "SQL Server Change Tracking", []string{"mssql+ct", "sqlserver+ct", "azuresql+ct", "azure-sql+ct"}, true, false),
		genericURIConnector("mongodb-cdc", "MongoDB CDC", []string{"mongodb+cdc", "mongodb+srv+cdc"}, true, false),
		{
			ID:            "azuresql",
			Name:          "Azure SQL",
			Schemes:       []string{"azuresql", "azure-sql"},
			IsSource:      true,
			IsDestination: false,
			Fields: []ConnectorField{
				{Name: "host", Label: "Server", Type: "string", Required: true, Placeholder: "myserver.database.windows.net"},
				{Name: "port", Label: "Port", Type: "number", Required: false, Default: "1433", Placeholder: "1433"},
				{Name: "user", Label: "Username / Client ID", Type: "string", Required: false},
				{Name: "password", Label: "Password / Token", Type: "password", Required: false},
				{Name: "database", Label: "Database", Type: "string", Required: true, Placeholder: "mydb"},
				{Name: "fedauth", Label: "Authentication", Type: "select", Required: false, Options: []FieldOption{
					{Value: "", Label: "SQL Authentication"},
					{Value: "ActiveDirectoryDefault", Label: "Microsoft Entra Default"},
					{Value: "ActiveDirectoryServicePrincipal", Label: "Service Principal"},
					{Value: "ActiveDirectoryManagedIdentity", Label: "Managed Identity"},
					{Value: "ActiveDirectoryServicePrincipalAccessToken", Label: "Access Token"},
					{Value: "ActiveDirectoryAzCli", Label: "Azure CLI"},
				}},
				{Name: "tenant_id", Label: "Tenant ID", Type: "string", Required: false},
				{Name: "encrypt", Label: "Encrypt", Type: "checkbox", Required: false, Default: "true"},
			},
		},
		{
			ID:            "mongodb",
			Name:          "MongoDB",
			Schemes:       []string{"mongodb", "mongodb+srv"},
			IsSource:      true,
			IsDestination: true,
			Fields: []ConnectorField{
				{Name: "host", Label: "Host", Type: "string", Required: true, Placeholder: "localhost"},
				{Name: "port", Label: "Port", Type: "number", Required: false, Default: "27017", Placeholder: "27017"},
				{Name: "user", Label: "Username", Type: "string", Required: false},
				{Name: "password", Label: "Password", Type: "password", Required: false},
				{Name: "database", Label: "Database", Type: "string", Required: true, Placeholder: "mydb"},
				{Name: "srv", Label: "Use SRV (mongodb+srv://)", Type: "checkbox", Required: false, Default: "false"},
			},
		},
		{
			ID:            "cassandra",
			Name:          "Cassandra",
			Schemes:       []string{"cassandra"},
			IsSource:      true,
			IsDestination: true,
			Fields: []ConnectorField{
				{Name: "host", Label: "Host", Type: "string", Required: true, Placeholder: "localhost"},
				{Name: "port", Label: "Port", Type: "number", Required: false, Default: "9042", Placeholder: "9042"},
				{Name: "user", Label: "Username", Type: "string", Required: false},
				{Name: "password", Label: "Password", Type: "password", Required: false},
				{Name: "database", Label: "Keyspace", Type: "string", Required: true, Placeholder: "analytics"},
			},
		},
		{
			ID:            "duckdb",
			Name:          "DuckDB",
			Schemes:       []string{"duckdb"},
			IsSource:      true,
			IsDestination: true,
			Fields: []ConnectorField{
				{Name: "path", Label: "Database Path", Type: "string", Required: true, Placeholder: "/path/to/database.db"},
			},
		},
		{
			ID:            "bigquery",
			Name:          "BigQuery",
			Schemes:       []string{"bigquery"},
			IsSource:      true,
			IsDestination: true,
			Fields: []ConnectorField{
				{Name: "project", Label: "Project ID", Type: "string", Required: true, Placeholder: "my-project"},
				{Name: "dataset", Label: "Dataset", Type: "string", Required: true, Placeholder: "my_dataset"},
				{Name: "credentials_path", Label: "Credentials File Path", Type: "string", Required: false, Placeholder: "/path/to/service-account.json"},
				{Name: "location", Label: "Location", Type: "string", Required: false, Default: "US", Placeholder: "US"},
			},
		},
		{
			ID:            "snowflake",
			Name:          "Snowflake",
			Schemes:       []string{"snowflake"},
			IsSource:      true,
			IsDestination: true,
			Fields: []ConnectorField{
				{Name: "account", Label: "Account", Type: "string", Required: true, Placeholder: "xy12345.us-east-1"},
				{Name: "user", Label: "Username", Type: "string", Required: true},
				{Name: "password", Label: "Password", Type: "password", Required: true},
				{Name: "database", Label: "Database", Type: "string", Required: true},
				{Name: "schema", Label: "Schema", Type: "string", Required: false, Default: "PUBLIC", Placeholder: "PUBLIC"},
				{Name: "warehouse", Label: "Warehouse", Type: "string", Required: false},
			},
		},
		{
			ID:            "sqlite",
			Name:          "SQLite",
			Schemes:       []string{"sqlite"},
			IsSource:      true,
			IsDestination: true,
			Fields: []ConnectorField{
				{Name: "path", Label: "Database Path", Type: "string", Required: true, Placeholder: "/path/to/database.db"},
			},
		},
		{
			ID:            "csv",
			Name:          "CSV File",
			Schemes:       []string{"csv"},
			IsSource:      true,
			IsDestination: true,
			Fields: []ConnectorField{
				{Name: "path", Label: "File Path", Type: "string", Required: true, Placeholder: "/path/to/file.csv"},
			},
		},
		{
			ID:            "parquet",
			Name:          "Parquet File",
			Schemes:       []string{"parquet"},
			IsSource:      true,
			IsDestination: true,
			Fields: []ConnectorField{
				{Name: "path", Label: "File Path", Type: "string", Required: true, Placeholder: "/path/to/file.parquet"},
			},
		},
		{
			ID:            "jsonl",
			Name:          "JSONL File",
			Schemes:       []string{"jsonl", "ndjson"},
			IsSource:      true,
			IsDestination: true,
			Fields: []ConnectorField{
				{Name: "path", Label: "File Path", Type: "string", Required: true, Placeholder: "/path/to/file.jsonl"},
			},
		},
		{
			ID:            "sftp",
			Name:          "SFTP",
			Schemes:       []string{"sftp"},
			IsSource:      true,
			IsDestination: false,
			Fields: []ConnectorField{
				{Name: "host", Label: "Host", Type: "string", Required: true, Placeholder: "sftp.example.com"},
				{Name: "port", Label: "Port", Type: "number", Required: false, Default: "22", Placeholder: "22"},
				{Name: "user", Label: "Username", Type: "string", Required: true},
				{Name: "password", Label: "Password", Type: "password", Required: true},
			},
		},
		{
			ID:            "hana",
			Name:          "SAP HANA",
			Schemes:       []string{"hana", "saphana"},
			IsSource:      true,
			IsDestination: false,
			Fields: []ConnectorField{
				{Name: "host", Label: "Host", Type: "string", Required: true, Placeholder: "localhost"},
				{Name: "port", Label: "Port", Type: "number", Required: false, Default: "30015", Placeholder: "30015"},
				{Name: "user", Label: "Username", Type: "string", Required: true},
				{Name: "password", Label: "Password", Type: "password", Required: true},
				{Name: "database", Label: "Database", Type: "string", Required: false},
			},
		},
	}

	connectors = append(
		connectors,
		genericURIConnector("adjust", "Adjust", []string{"adjust"}, true, false),
		genericURIConnector("airtable", "Airtable", []string{"airtable"}, true, false),
		genericURIConnector("allium", "Allium", []string{"allium"}, true, false),
		genericURIConnector("anthropic", "Anthropic", []string{"anthropic"}, true, false),
		genericURIConnector("apifootball", "API Football", []string{"apifootball"}, true, false),
		genericURIConnector("appleads", "Apple Ads", []string{"appleads"}, true, false),
		genericURIConnector("applovin", "AppLovin", []string{"applovin"}, true, false),
		genericURIConnector("applovinmax", "AppLovin MAX", []string{"applovinmax"}, true, false),
		genericURIConnector("appsflyer", "AppsFlyer", []string{"appsflyer"}, true, false),
		genericURIConnector("appstore", "App Store Connect", []string{"appstore"}, true, false),
		genericURIConnector("arrowstream", "Arrow Stream", []string{"arrow-stream", "arrowstream"}, true, false),
		genericURIConnector("asana", "Asana", []string{"asana"}, true, false),
		genericURIConnector("athena", "Amazon Athena", []string{"athena"}, true, true),
		genericURIConnector("attio", "Attio", []string{"attio"}, true, false),
		genericURIConnector("avro", "Avro File", []string{"avro"}, true, false),
		genericURIConnector("balldontlie", "balldontlie", []string{"balldontlie"}, true, false),
		genericURIConnector("blobstore", "Object Storage", []string{"s3", "gs", "gcs", "az", "azure", "adls", "adlsgen2", "azdatalake", "abfs", "abfss"}, true, true),
		genericURIConnector("braze", "Braze", []string{"braze"}, true, false),
		genericURIConnector("bruin", "Bruin", []string{"bruin"}, true, false),
		genericURIConnector("chargebee", "Chargebee", []string{"chargebee"}, true, false),
		genericURIConnector("chess", "Chess.com", []string{"chess"}, true, false),
		genericURIConnector("clickhouse", "ClickHouse", []string{"clickhouse"}, true, true),
		genericURIConnector("clickup", "ClickUp", []string{"clickup"}, true, false),
		genericURIConnector("couchbase", "Couchbase", []string{"couchbase"}, true, false),
		genericURIConnector("cratedb", "CrateDB", []string{"cratedb"}, true, true),
		genericURIConnector("cursor", "Cursor", []string{"cursor"}, true, false),
		genericURIConnector("customerio", "Customer.io", []string{"customerio"}, true, false),
		genericURIConnector("databricks", "Databricks", []string{"databricks"}, true, true),
		genericURIConnector("db2", "Db2", []string{"db2", "ibmdb2"}, true, false),
		genericURIConnector("discard", "Discard", []string{"discard"}, false, true),
		genericURIConnector("docebo", "Docebo", []string{"docebo"}, true, false),
		genericURIConnector("ducklake", "DuckLake", []string{"ducklake"}, true, true),
		genericURIConnector("dune", "Dune", []string{"dune"}, true, false),
		genericURIConnector("dynamodb", "DynamoDB", []string{"dynamodb"}, true, true),
		genericURIConnector("elasticsearch", "Elasticsearch", []string{"elasticsearch"}, true, true),
		genericURIConnector("espn", "ESPN", []string{"espn"}, true, false),
		genericURIConnector("eventhubs", "Azure Event Hubs", []string{"eventhubs", "eventhub", "azure-event-hubs", "azureeventhubs"}, true, false),
		genericURIConnector("fabric", "Microsoft Fabric", []string{"fabric"}, true, true),
		genericURIConnector("facebookads", "Facebook Ads", []string{"facebookads"}, true, false),
		genericURIConnector("fireflies", "Fireflies", []string{"fireflies"}, true, false),
		genericURIConnector("fluxx", "Fluxx", []string{"fluxx"}, true, false),
		genericURIConnector("footballdata", "Football Data", []string{"footballdata"}, true, false),
		genericURIConnector("frankfurter", "Frankfurter", []string{"frankfurter"}, true, false),
		genericURIConnector("freshdesk", "Freshdesk", []string{"freshdesk"}, true, false),
		genericURIConnector("fundraiseup", "Fundraise Up", []string{"fundraiseup"}, true, false),
		genericURIConnector("g2", "G2", []string{"g2"}, true, false),
		genericURIConnector("github", "GitHub", []string{"github"}, true, false),
		genericURIConnector("gitlab", "GitLab", []string{"gitlab"}, true, false),
		genericURIConnector("googleanalytics", "Google Analytics", []string{"googleanalytics"}, true, false),
		genericURIConnector("googleads", "Google Ads", []string{"googleads"}, true, false),
		genericURIConnector("gsheets", "Google Sheets", []string{"gsheets"}, true, false),
		genericURIConnector("gorgias", "Gorgias", []string{"gorgias"}, true, false),
		genericURIConnector("granola", "Granola", []string{"granola"}, true, false),
		genericURIConnector("hostaway", "Hostaway", []string{"hostaway"}, true, false),
		genericURIConnector("http", "HTTP", []string{"http", "https"}, true, false),
		genericURIConnector("hubspot", "HubSpot", []string{"hubspot"}, true, false),
		genericURIConnector("indeed", "Indeed", []string{"indeed"}, true, false),
		genericURIConnector("influxdb", "InfluxDB", []string{"influxdb"}, true, false),
		genericURIConnector("intercom", "Intercom", []string{"intercom"}, true, false),
		genericURIConnector("isoc-pulse", "ISOC Pulse", []string{"isoc-pulse"}, true, false),
		genericURIConnector("jira", "Jira", []string{"jira"}, true, false),
		genericURIConnector("jobtread", "JobTread", []string{"jobtread"}, true, false),
		genericURIConnector("json", "JSON File", []string{"json"}, true, false),
		genericURIConnector("kafka", "Kafka", []string{"kafka"}, true, false),
		genericURIConnector("kalshi", "Kalshi", []string{"kalshi"}, true, false),
		genericURIConnector("kinesis", "Kinesis", []string{"kinesis"}, true, false),
		genericURIConnector("klaviyo", "Klaviyo", []string{"klaviyo"}, true, false),
		genericURIConnector("linear", "Linear", []string{"linear"}, true, false),
		genericURIConnector("linkedinads", "LinkedIn Ads", []string{"linkedinads"}, true, false),
		genericURIConnector("mailchimp", "Mailchimp", []string{"mailchimp"}, true, false),
		genericURIConnector("manifold", "Manifold", []string{"manifold"}, true, false),
		genericURIConnector("maxcompute", "MaxCompute", []string{"maxcompute", "odps"}, true, true),
		genericURIConnector("mixpanel", "Mixpanel", []string{"mixpanel"}, true, false),
		genericURIConnector("mmap", "Memory-mapped File", []string{"mmap"}, true, false),
		genericURIConnector("monday", "monday.com", []string{"monday"}, true, false),
		genericURIConnector("motherduck", "MotherDuck", []string{"motherduck", "md"}, true, true),
		genericURIConnector("nats", "NATS JetStream", []string{"nats"}, true, false),
		genericURIConnector("notion", "Notion", []string{"notion"}, true, false),
		genericURIConnector("onelake", "OneLake", []string{"onelake"}, false, true),
		genericURIConnector("oracle", "Oracle", []string{"oracle", "oracle+cx_oracle"}, true, false),
		genericURIConnector("paddle", "Paddle", []string{"paddle"}, true, false),
		genericURIConnector("personio", "Personio", []string{"personio"}, true, false),
		genericURIConnector("phantombuster", "PhantomBuster", []string{"phantombuster"}, true, false),
		genericURIConnector("pinterest", "Pinterest", []string{"pinterest"}, true, false),
		genericURIConnector("pipedrive", "Pipedrive", []string{"pipedrive"}, true, false),
		genericURIConnector("plusvibeai", "PlusVibe AI", []string{"plusvibeai"}, true, false),
		genericURIConnector("polymarket", "Polymarket", []string{"polymarket"}, true, false),
		genericURIConnector("posthog", "PostHog", []string{"posthog"}, true, false),
		genericURIConnector("primer", "Primer", []string{"primer"}, true, false),
		genericURIConnector("pulsar", "Apache Pulsar", []string{"pulsar", "pulsar+ssl"}, true, false),
		genericURIConnector("pubsub", "Pub/Sub", []string{"pubsub"}, true, false),
		genericURIConnector("quickbooks", "QuickBooks", []string{"quickbooks"}, true, false),
		genericURIConnector("rabbitmq", "RabbitMQ", []string{"amqp", "amqps"}, true, false),
		genericURIConnector("redditads", "Reddit Ads", []string{"redditads"}, true, false),
		genericURIConnector("redis", "Redis Streams", []string{"redis", "rediss"}, true, false),
		genericURIConnector("redshift", "Redshift", []string{"redshift", "redshift+psycopg2"}, true, true),
		genericURIConnector("revenuecat", "RevenueCat", []string{"revenuecat"}, true, false),
		genericURIConnector("salesforce", "Salesforce", []string{"salesforce"}, true, false),
		genericURIConnector("sendgrid", "SendGrid", []string{"sendgrid"}, true, false),
		genericURIConnector("sharepoint", "SharePoint", []string{"sharepoint"}, true, false),
		genericURIConnector("shopify", "Shopify", []string{"shopify"}, true, false),
		genericURIConnector("slack", "Slack", []string{"slack"}, true, false),
		genericURIConnector("smartsheet", "Smartsheet", []string{"smartsheet"}, true, false),
		genericURIConnector("snapchatads", "Snapchat Ads", []string{"snapchatads"}, true, false),
		genericURIConnector("socrata", "Socrata", []string{"socrata"}, true, false),
		genericURIConnector("solidgate", "Solidgate", []string{"solidgate"}, true, false),
		genericURIConnector("spanner", "Spanner", []string{"spanner"}, true, false),
		genericURIConnector("sqs", "SQS", []string{"sqs"}, true, false),
		genericURIConnector("square", "Square", []string{"square"}, true, false),
		genericURIConnector("stripe", "Stripe", []string{"stripe"}, true, false),
		genericURIConnector("surveymonkey", "SurveyMonkey", []string{"surveymonkey"}, true, false),
		genericURIConnector("synapse", "Azure Synapse", []string{"synapse"}, false, true),
		genericURIConnector("tiktok", "TikTok Ads", []string{"tiktok"}, true, false),
		genericURIConnector("trino", "Trino", []string{"trino"}, true, true),
		genericURIConnector("trustpilot", "Trustpilot", []string{"trustpilot"}, true, false),
		genericURIConnector("twilio", "Twilio", []string{"twilio"}, true, false),
		genericURIConnector("wise", "Wise", []string{"wise"}, true, false),
		genericURIConnector("wistia", "Wistia", []string{"wistia"}, true, false),
		genericURIConnector("zendesk", "Zendesk", []string{"zendesk"}, true, false),
		genericURIConnector("zoom", "Zoom", []string{"zoom"}, true, false),
	)

	return connectors
}

func genericURIConnector(id string, name string, schemes []string, isSource bool, isDestination bool) ConnectorType {
	return ConnectorType{
		ID:            id,
		Name:          name,
		Schemes:       schemes,
		IsSource:      isSource,
		IsDestination: isDestination,
		Fields: []ConnectorField{
			{
				Name:        "uri",
				Label:       "URI",
				Type:        "string",
				Required:    true,
				Placeholder: schemes[0] + "://...",
			},
		},
	}
}

func GetConnectorByID(id string) *ConnectorType {
	for _, c := range GetConnectors() {
		if c.ID == id {
			return &c
		}
	}
	return nil
}

func BuildURI(connectorID string, fields map[string]string) string {
	if raw := strings.TrimSpace(fields["uri"]); raw != "" {
		return raw
	}

	switch connectorID {
	case "postgres":
		return buildStandardURI("postgres", fields, "5432")
	case "mysql":
		return buildStandardURI("mysql", fields, "3306")
	case "mssql":
		return buildStandardURI("mssql", fields, "1433")
	case "azuresql":
		return buildAzureSQLURI(fields)
	case "mongodb":
		scheme := "mongodb"
		if fields["srv"] == "true" {
			scheme = "mongodb+srv"
		}
		return buildStandardURI(scheme, fields, "27017")
	case "cassandra":
		return buildStandardURI("cassandra", fields, "9042")
	case "duckdb":
		return "duckdb:///" + fields["path"]
	case "sqlite":
		return "sqlite:///" + fields["path"]
	case "csv":
		return "csv://" + fields["path"]
	case "parquet":
		return "parquet://" + fields["path"]
	case "jsonl":
		return "jsonl://" + fields["path"]
	case "bigquery":
		uri := "bigquery://" + fields["project"] + "/" + fields["dataset"]
		params := []string{}
		if fields["credentials_path"] != "" {
			params = append(params, "credentials_path="+fields["credentials_path"])
		}
		if fields["location"] != "" {
			params = append(params, "location="+fields["location"])
		}
		if len(params) > 0 {
			uri += "?"
			for i, p := range params {
				if i > 0 {
					uri += "&"
				}
				uri += p
			}
		}
		return uri
	case "snowflake":
		uri := "snowflake://" + fields["user"] + ":" + fields["password"] + "@" + fields["account"] + "/" + fields["database"]
		if fields["schema"] != "" {
			uri += "/" + fields["schema"]
		}
		params := []string{}
		if fields["warehouse"] != "" {
			params = append(params, "warehouse="+fields["warehouse"])
		}
		if len(params) > 0 {
			uri += "?"
			for i, p := range params {
				if i > 0 {
					uri += "&"
				}
				uri += p
			}
		}
		return uri
	case "sftp":
		return buildStandardURI("sftp", fields, "22")
	case "hana":
		return buildStandardURI("hana", fields, "30015")
	default:
		return ""
	}
}

func buildAzureSQLURI(fields map[string]string) string {
	host := fields["host"]
	port := fields["port"]
	if port == "" {
		port = "1433"
	}

	u := &url.URL{
		Scheme: "azuresql",
		Host:   host + ":" + port,
	}
	if fields["user"] != "" || fields["password"] != "" {
		if fields["password"] != "" {
			u.User = url.UserPassword(fields["user"], fields["password"])
		} else {
			u.User = url.User(fields["user"])
		}
	}
	if fields["database"] != "" {
		u.Path = "/" + fields["database"]
	}

	query := url.Values{}
	if fields["fedauth"] != "" {
		query.Set("fedauth", fields["fedauth"])
	}
	if fields["tenant_id"] != "" {
		query.Set("tenant_id", fields["tenant_id"])
	}
	if fields["encrypt"] == "" {
		query.Set("encrypt", "true")
	} else {
		query.Set("encrypt", fields["encrypt"])
	}
	u.RawQuery = query.Encode()

	return u.String()
}

func buildStandardURI(scheme string, fields map[string]string, defaultPort string) string {
	uri := scheme + "://"
	if fields["user"] != "" {
		uri += fields["user"]
		if fields["password"] != "" {
			uri += ":" + fields["password"]
		}
		uri += "@"
	}
	uri += fields["host"]
	port := fields["port"]
	if port == "" {
		port = defaultPort
	}
	uri += ":" + port
	if fields["database"] != "" {
		uri += "/" + fields["database"]
	}
	if fields["sslmode"] != "" && scheme == "postgres" {
		uri += "?sslmode=" + fields["sslmode"]
	}
	return uri
}
