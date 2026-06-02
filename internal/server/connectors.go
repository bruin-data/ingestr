package server

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
	return []ConnectorType{
		{
			ID:            "postgres",
			Name:          "PostgreSQL",
			Schemes:       []string{"postgres", "postgresql"},
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
			Schemes:       []string{"mysql", "mariadb"},
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
			Schemes:       []string{"mssql", "sqlserver"},
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
	switch connectorID {
	case "postgres":
		return buildStandardURI("postgres", fields, "5432")
	case "mysql":
		return buildStandardURI("mysql", fields, "3306")
	case "mssql":
		return buildStandardURI("mssql", fields, "1433")
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
