# Couchbase Source Implementation

Comprehensive Couchbase data source connector for ingesting data from Couchbase databases using both N1QL queries and Key-Value operations.

## Features

### Core Capabilities
- **N1QL Query Support**: Execute complex N1QL queries for flexible data retrieval
- **Key-Value Operations**: Direct document access using KV operations for high performance
- **Incremental Loading**: Support for incremental data loads using cursor fields
- **Custom Query Support**: Execute custom N1QL queries with parameter substitution
- **Projection Support**: Select specific fields to optimize data transfer
- **Filtering**: Apply filters to narrow down data selection
- **Chunked Processing**: Process large datasets in configurable chunks
- **Multiple Scopes and Collections**: Support for Couchbase's hierarchical data model

### Connection Options
- Standard Couchbase connections (`couchbase://`)
- Secure connections with TLS (`couchbases://`)
- Connection string with query parameters
- Automatic connection retry and timeout handling

## URI Format

### Basic Format
```
couchbase://username:password@host:port/bucket?scope=scope_name&collection=collection_name
```

### Secure Connection
```
couchbases://username:password@host:port/bucket?scope=scope_name&collection=collection_name
```

### Examples
```
# Basic connection with default scope
couchbase://admin:password@localhost:11210/travel-sample

# Connection with specific scope and collection
couchbase://admin:password@localhost:11210/travel-sample?scope=inventory&collection=airline

# Secure connection to Couchbase Cloud
couchbases://user:pass@cluster.cloud.couchbase.com:11207/mybucket?scope=myscope&collection=mycoll
```

## Usage Examples

### 1. Basic Collection Loading

Load all documents from a collection:

```python
from ingestr.src.couchbase import couchbase_collection

# Load entire collection
resource = couchbase_collection(
    connection_string="couchbase://localhost",
    username="admin",
    password="password",
    bucket="travel-sample",
    scope="inventory",
    collection="airline"
)
```

### 2. Incremental Loading

Load documents incrementally based on a timestamp field:

```python
import pendulum
from dlt.sources import incremental

resource = couchbase_collection(
    connection_string="couchbase://localhost",
    username="admin",
    password="password",
    bucket="travel-sample",
    scope="inventory",
    collection="airline",
    incremental=incremental(
        cursor_path="updated_at",
        initial_value=pendulum.parse("2023-01-01T00:00:00Z")
    )
)
```

### 3. Custom N1QL Query

Execute a custom N1QL query:

```python
custom_query = """
SELECT META().id as _id, airline.*, country.name as country_name
FROM `travel-sample`.`inventory`.`airline` as airline
LEFT JOIN `travel-sample`.`inventory`.`country` as country
  ON airline.country = country.code
WHERE airline.type = 'airline'
  AND airline.country = 'United States'
ORDER BY airline.name
"""

resource = couchbase_collection(
    connection_string="couchbase://localhost",
    username="admin",
    password="password",
    bucket="travel-sample",
    scope="inventory",
    collection="airline",
    custom_query=custom_query
)
```

### 4. Field Projection

Select specific fields to reduce data transfer:

```python
resource = couchbase_collection(
    connection_string="couchbase://localhost",
    username="admin",
    password="password",
    bucket="travel-sample",
    scope="inventory",
    collection="airline",
    projection=["name", "iata", "icao", "country"]
)
```

### 5. Filtering

Apply filters to narrow down results:

```python
resource = couchbase_collection(
    connection_string="couchbase://localhost",
    username="admin",
    password="password",
    bucket="travel-sample",
    scope="inventory",
    collection="airline",
    filter_={"country": "United States", "type": "airline"}
)
```

### 6. Key-Value Mode

Load documents by specific keys using KV operations:

```python
resource = couchbase_collection(
    connection_string="couchbase://localhost",
    username="admin",
    password="password",
    bucket="travel-sample",
    scope="inventory",
    collection="airline",
    kv_mode=True,
    keys=["airline_10", "airline_11", "airline_12"]
)
```

### 7. Multiple Collections

Load multiple collections from a scope:

```python
from ingestr.src.couchbase import couchbase

source = couchbase(
    connection_string="couchbase://localhost",
    username="admin",
    password="password",
    bucket="travel-sample",
    scope="inventory",
    collection_names=["airline", "airport", "hotel"]
)
```

## Table Format Specification

The `table` parameter in the ingestr CLI supports multiple formats:

### 1. Simple Collection Name
```bash
collection_name
```
Example: `airline`

### 2. Scope and Collection
```bash
scope.collection
```
Example: `inventory.airline`

### 3. Full Path
```bash
bucket.scope.collection
```
Example: `travel-sample.inventory.airline`

### 4. Custom Query
```bash
collection_name:custom_n1ql_query
```
Example: `airline:SELECT * FROM \`travel-sample\`.\`inventory\`.\`airline\` WHERE country = 'US'`

## Configuration Parameters

### Connection Parameters
- `connection_string` (str): Couchbase cluster connection string
- `username` (str): Couchbase username
- `password` (str): Couchbase password
- `bucket` (str): Bucket name
- `scope` (str, optional): Scope name (defaults to "_default")
- `collection` (str): Collection name

### Query Parameters
- `filter_` (dict, optional): Dictionary of field:value pairs to filter documents
- `projection` (list, optional): List of field names to include in results
- `custom_query` (str, optional): Custom N1QL query to execute
- `limit` (int, optional): Maximum number of documents to load
- `chunk_size` (int, optional): Number of documents to process in each batch (default: 10000)

### Incremental Loading Parameters
- `incremental` (dlt.sources.incremental, optional): Incremental loading configuration
- `write_disposition` (str, optional): Write disposition ("append", "replace", "merge")

### Key-Value Mode Parameters
- `kv_mode` (bool, optional): Use Key-Value operations instead of N1QL (default: False)
- `keys` (list, optional): List of document keys to load in KV mode

## Use Cases

### 1. Data Warehouse ETL
Extract data from Couchbase operational database to analytical data warehouse:
```python
# Incremental sync of order data
resource = couchbase_collection(
    connection_string="couchbase://prod-cluster",
    username="etl_user",
    password="secret",
    bucket="ecommerce",
    scope="orders",
    collection="transactions",
    incremental=incremental("order_date", initial_value=yesterday),
    projection=["order_id", "customer_id", "amount", "status", "order_date"]
)
```

### 2. Real-time Analytics
Stream data for real-time analytics:
```python
# Monitor recent user activities
custom_query = """
SELECT META().id as _id, activity.*
FROM `analytics`.`tracking`.`user_activity` as activity
WHERE activity.timestamp >= $start_time
ORDER BY activity.timestamp DESC
"""

resource = couchbase_collection(
    connection_string="couchbase://analytics-cluster",
    username="analytics_user",
    password="secret",
    bucket="analytics",
    scope="tracking",
    collection="user_activity",
    custom_query=custom_query,
    chunk_size=5000
)
```

### 3. Data Migration
Migrate data between Couchbase clusters:
```python
# Export all documents for migration
source = couchbase(
    connection_string="couchbase://old-cluster",
    username="admin",
    password="password",
    bucket="myapp",
    scope="data",
    collection_names=None  # All collections in scope
)
```

### 4. Audit and Compliance
Extract audit logs for compliance reporting:
```python
# Extract audit logs with filters
resource = couchbase_collection(
    connection_string="couchbase://audit-cluster",
    username="audit_reader",
    password="secret",
    bucket="audit",
    scope="logs",
    collection="user_actions",
    filter_={"action_type": "sensitive_data_access"},
    incremental=incremental("timestamp", initial_value=last_week)
)
```

### 5. Machine Learning Feature Store
Extract features for ML model training:
```python
# Extract user features
custom_query = """
SELECT
    u.user_id,
    u.age,
    u.location,
    COUNT(o.order_id) as order_count,
    AVG(o.amount) as avg_order_amount
FROM `ecommerce`.`users`.`profiles` u
LEFT JOIN `ecommerce`.`orders`.`transactions` o
    ON u.user_id = o.user_id
GROUP BY u.user_id, u.age, u.location
"""

resource = couchbase_collection(
    connection_string="couchbase://ml-cluster",
    username="ml_user",
    password="secret",
    bucket="ecommerce",
    collection="profiles",
    custom_query=custom_query
)
```

### 6. Document Backup by Key
Backup specific documents using KV operations:
```python
# Backup critical documents
critical_keys = ["user:vip:123", "user:vip:456", "user:vip:789"]

resource = couchbase_collection(
    connection_string="couchbase://prod",
    username="backup_user",
    password="secret",
    bucket="users",
    scope="vip",
    collection="profiles",
    kv_mode=True,
    keys=critical_keys
)
```

## Performance Considerations

### Chunk Size
- Default: 10,000 documents per chunk
- Larger chunks: Better for high-throughput scenarios
- Smaller chunks: Better for memory-constrained environments

### N1QL vs Key-Value
- **N1QL**: Use for complex queries, filtering, and aggregations
- **Key-Value**: Use for direct document access by ID (faster, lower latency)

### Indexes
Ensure proper indexes exist for:
- Incremental cursor fields
- Filter fields
- Fields used in WHERE clauses

### Connection Pooling
The connector automatically manages connection pooling and retries.

## Error Handling

The connector handles common errors:
- Connection timeouts
- Authentication failures
- Document not found (in KV mode)
- Query timeout
- Invalid N1QL syntax

## Data Type Conversions

Couchbase types are automatically converted to Python types:
- Datetime → Pendulum datetime
- Timedelta → Seconds (float)
- All other types preserved as-is

## Security Best Practices

1. **Use Secure Connections**: Prefer `couchbases://` for production
2. **Credential Management**: Store credentials in environment variables or secrets manager
3. **Least Privilege**: Use read-only users for data extraction
4. **Connection Timeouts**: Set appropriate timeouts to prevent hanging connections
5. **TLS Certificates**: Validate server certificates in production environments

## Troubleshooting

### Connection Issues
```python
# Add connection timeout
resource = couchbase_collection(
    connection_string="couchbase://localhost",
    username="admin",
    password="password",
    bucket="mybucket",
    collection="mycollection",
    # Additional cluster options can be passed to cluster_from_credentials
)
```

### Query Timeout
For long-running queries, increase the query timeout in the custom query or use chunking.

### Memory Issues
Reduce `chunk_size` parameter:
```python
resource = couchbase_collection(
    ...,
    chunk_size=1000  # Smaller chunks for memory-constrained environments
)
```

## Dependencies

Required Python packages:
- `couchbase>=4.0.0` - Couchbase Python SDK
- `dlt>=0.3.0` - Data Load Tool framework
- `pendulum>=2.0.0` - Date/time handling

Install with:
```bash
pip install couchbase dlt pendulum
```

## Testing

Run tests with:
```bash
pytest ingestr/src/couchbase/helpers_test.py -v
```

## License

This implementation is part of the ingestr project and follows the same license terms.
