# Anthropic

[Anthropic](https://www.anthropic.com/) is an AI safety company that builds Claude, a family of large language models. This source enables you to extract comprehensive data from the Anthropic Admin API, including Claude Code usage metrics, API usage reports, cost data, and organization management information.

## URI Format

The URI format for Anthropic is:

```plaintext
anthropic://?api_key=<admin_api_key>
```

### URI Parameters:

- `api_key` (required): Your Anthropic Admin API key (must start with `sk-ant-admin...`)

::: warning Admin API Key Required
This source requires an **Admin API key** which is different from standard API keys. Only organization members with the admin role can provision Admin API keys through the [Anthropic Console](https://console.anthropic.com/settings/admin-keys).

The Admin API is unavailable for individual accounts. To use this source, you must have an organization set up in Console → Settings → Organization.
:::

## Available Tables

### claude_code_usage

The `claude_code_usage` table contains daily aggregated usage metrics for Claude Code users in your organization. This data helps you analyze developer productivity and monitor Claude Code adoption.

#### Schema

| Column | Type | Description |
|--------|------|-------------|
| `date` | `string` | Date in RFC 3339 format (UTC timestamp) |
| `actor_type` | `string` | Type of actor (`user_actor` or `api_actor`) |
| `actor_id` | `string` | Email address for users or API key name for API actors |
| `organization_id` | `string` | Organization UUID |
| `customer_type` | `string` | Type of customer account (`api` or `subscription`) |
| `terminal_type` | `string` | Terminal/environment where Claude Code was used (e.g., `vscode`, `iTerm.app`) |
| `num_sessions` | `integer` | Number of distinct Claude Code sessions |
| `lines_added` | `integer` | Total lines of code added across all files |
| `lines_removed` | `integer` | Total lines of code removed across all files |
| `commits_by_claude_code` | `integer` | Number of git commits created through Claude Code |
| `pull_requests_by_claude_code` | `integer` | Number of pull requests created through Claude Code |
| `edit_tool_accepted` | `integer` | Number of Edit tool proposals accepted |
| `edit_tool_rejected` | `integer` | Number of Edit tool proposals rejected |
| `multi_edit_tool_accepted` | `integer` | Number of MultiEdit tool proposals accepted |
| `multi_edit_tool_rejected` | `integer` | Number of MultiEdit tool proposals rejected |
| `write_tool_accepted` | `integer` | Number of Write tool proposals accepted |
| `write_tool_rejected` | `integer` | Number of Write tool proposals rejected |
| `notebook_edit_tool_accepted` | `integer` | Number of NotebookEdit tool proposals accepted |
| `notebook_edit_tool_rejected` | `integer` | Number of NotebookEdit tool proposals rejected |
| `total_input_tokens` | `integer` | Total input tokens across all models |
| `total_output_tokens` | `integer` | Total output tokens across all models |
| `total_cache_read_tokens` | `integer` | Total cache read tokens across all models |
| `total_cache_creation_tokens` | `integer` | Total cache creation tokens across all models |
| `total_estimated_cost_cents` | `integer` | Total estimated cost in cents USD |
| `models_used` | `string` | Comma-separated list of Claude models used |

### usage_report

The `usage_report` table contains detailed token usage metrics from the Messages API, aggregated by time bucket, workspace, API key, model, and service tier.

#### Schema

| Column | Type | Description |
|--------|------|-------------|
| `bucket` | `string` | Time bucket in ISO 8601 format |
| `api_key_id` | `string` | API key identifier |
| `workspace_id` | `string` | Workspace identifier |
| `model` | `string` | Claude model used |
| `service_tier` | `string` | Service tier (scale or default) |
| `input_tokens` | `integer` | Number of input tokens |
| `output_tokens` | `integer` | Number of output tokens |
| `input_cached_tokens` | `integer` | Number of cached input tokens |
| `api_first_response_latency_ms_p50` | `float` | 50th percentile first response latency in milliseconds |
| `api_first_response_latency_ms_p95` | `float` | 95th percentile first response latency in milliseconds |
| `api_first_response_latency_ms_p99` | `float` | 99th percentile first response latency in milliseconds |
| `api_total_latency_ms_p50` | `float` | 50th percentile total latency in milliseconds |
| `api_total_latency_ms_p95` | `float` | 95th percentile total latency in milliseconds |
| `api_total_latency_ms_p99` | `float` | 99th percentile total latency in milliseconds |
| `api_request_count` | `integer` | Number of API requests |
| `server_tool_search_count` | `integer` | Number of server tool searches |
| `server_tool_result_count` | `integer` | Number of server tool results |

### cost_report

The `cost_report` table contains aggregated cost data broken down by workspace and cost description.

#### Schema

| Column | Type | Description |
|--------|------|-------------|
| `bucket` | `string` | Time bucket in ISO 8601 format |
| `workspace_id` | `string` | Workspace identifier |
| `description` | `string` | Cost description (e.g., "Usage - claude-3-5-sonnet-20241022") |
| `amount_cents` | `integer` | Cost amount in cents USD |

### organization

The `organization` table contains information about your Anthropic organization.

#### Schema

| Column | Type | Description |
|--------|------|-------------|
| `id` | `string` | Organization UUID |
| `name` | `string` | Organization name |
| `settings` | `object` | Organization settings (JSON) |
| `created_at` | `string` | Creation timestamp |

### workspaces

The `workspaces` table contains all workspaces in your organization.

#### Schema

| Column | Type | Description |
|--------|------|-------------|
| `id` | `string` | Workspace UUID |
| `name` | `string` | Workspace name |
| `type` | `string` | Workspace type (default or custom) |
| `created_at` | `string` | Creation timestamp |

### api_keys

The `api_keys` table contains all API keys in your organization.

#### Schema

| Column | Type | Description |
|--------|------|-------------|
| `id` | `string` | API key UUID |
| `name` | `string` | API key name |
| `status` | `string` | API key status (active or disabled) |
| `created_at` | `string` | Creation timestamp |
| `workspace_id` | `string` | Associated workspace UUID |
| `created_by_user_id` | `string` | User who created the key |
| `last_used_at` | `string` | Last usage timestamp |

### invites

The `invites` table contains all pending organization invites.

#### Schema

| Column | Type | Description |
|--------|------|-------------|
| `id` | `string` | Invite UUID |
| `email` | `string` | Invitee email address |
| `role` | `string` | Invited role (admin or member) |
| `expires_at` | `string` | Expiration timestamp |
| `workspace_ids` | `array` | List of workspace UUIDs |
| `created_at` | `string` | Creation timestamp |
| `created_by_user_id` | `string` | User who created the invite |

### users

The `users` table contains all users in your organization.

#### Schema

| Column | Type | Description |
|--------|------|-------------|
| `id` | `string` | User UUID |
| `email` | `string` | User email address |
| `name` | `string` | User full name |
| `role` | `string` | User role (admin or member) |
| `created_at` | `string` | Creation timestamp |
| `last_login_at` | `string` | Last login timestamp |

### workspace_members

The `workspace_members` table contains workspace membership information.

#### Schema

| Column | Type | Description |
|--------|------|-------------|
| `workspace_id` | `string` | Workspace UUID |
| `user_id` | `string` | User UUID |
| `role` | `string` | Role in workspace |
| `added_at` | `string` | When user was added to workspace |

## Examples

### Basic Usage

Load all Claude Code usage data to a DuckDB database:

```bash
ingestr ingest \
    --source-uri "anthropic://?api_key=sk-ant-admin-..." \
    --source-table "claude_code_usage" \
    --dest-uri "duckdb:///anthropic_data.db" \
    --dest-table "claude_code_usage"
```

### Incremental Loading

Load data incrementally starting from a specific date:

```bash
ingestr ingest \
    --source-uri "anthropic://?api_key=sk-ant-admin-..." \
    --source-table "claude_code_usage" \
    --dest-uri "postgresql://user:password@localhost:5432/analytics" \
    --dest-table "claude_code_usage" \
    --interval-start "2024-01-01" \
    --interval-end "2024-12-31"
```

### Load to BigQuery

```bash
ingestr ingest \
    --source-uri "anthropic://?api_key=sk-ant-admin-..." \
    --source-table "claude_code_usage" \
    --dest-uri "bigquery://project-id.dataset" \
    --dest-table "claude_code_usage"
```

### Load Organization Data

```bash
ingestr ingest \
    --source-uri "anthropic://?api_key=sk-ant-admin-..." \
    --source-table "organization" \
    --dest-uri "duckdb:///anthropic_data.db" \
    --dest-table "organization"
```

### Load Usage Report

```bash
ingestr ingest \
    --source-uri "anthropic://?api_key=sk-ant-admin-..." \
    --source-table "usage_report" \
    --dest-uri "postgresql://user:password@localhost:5432/analytics" \
    --dest-table "api_usage" \
    --interval-start "2024-01-01" \
    --interval-end "2024-12-31"
```

### Load Cost Report

```bash
ingestr ingest \
    --source-uri "anthropic://?api_key=sk-ant-admin-..." \
    --source-table "cost_report" \
    --dest-uri "duckdb:///costs.db" \
    --dest-table "anthropic_costs"
```

### Load All Users and Workspaces

```bash
# Load users
ingestr ingest \
    --source-uri "anthropic://?api_key=sk-ant-admin-..." \
    --source-table "users" \
    --dest-uri "duckdb:///org_data.db" \
    --dest-table "users"

# Load workspaces
ingestr ingest \
    --source-uri "anthropic://?api_key=sk-ant-admin-..." \
    --source-table "workspaces" \
    --dest-uri "duckdb:///org_data.db" \
    --dest-table "workspaces"

# Load workspace members
ingestr ingest \
    --source-uri "anthropic://?api_key=sk-ant-admin-..." \
    --source-table "workspace_members" \
    --dest-uri "duckdb:///org_data.db" \
    --dest-table "workspace_members"
```

## Incremental Loading

The following tables support incremental loading:
- `claude_code_usage` - incremental based on the `date` field
- `usage_report` - supports date range filtering with `--interval-start` and `--interval-end`
- `cost_report` - supports date range filtering with `--interval-start` and `--interval-end`

Other tables (`organization`, `workspaces`, `api_keys`, `invites`, `users`, `workspace_members`) use full refresh mode as they represent current state data.

When running incremental loads:
- The source tracks the last loaded date for `claude_code_usage`
- Subsequent runs will only fetch new data
- Use `--interval-start` and `--interval-end` to specify a custom date range
- Default start date is January 1, 2023

## Use Cases

### Developer Productivity Analysis

Track how your team uses Claude Code:
- Monitor adoption rates across different teams
- Analyze code generation patterns
- Track tool acceptance rates

### Cost Monitoring

Monitor Claude Code costs:
- Track token usage by user and model
- Analyze spending patterns
- Allocate costs by team or project

### Executive Dashboards

Create reports showing:
- Claude Code impact on development velocity
- Lines of code generated vs. manual coding
- Commit and PR creation metrics
- API usage patterns across workspaces
- Cost allocation by team and project

### Organization Management

Monitor and audit your organization:
- Track user access and permissions
- Monitor API key usage and lifecycle
- Audit workspace memberships
- Track pending invites and onboarding

## Data Freshness

Claude Code analytics data typically appears within 1 hour of user activity completion. The API provides daily aggregated metrics only.

## Rate Limits

The Anthropic Admin API has rate limits in place. The source handles pagination automatically and respects these limits.

## Notes

- This source only tracks Claude Code usage on the Anthropic API (1st party)
- Usage on Amazon Bedrock, Google Vertex AI, or other third-party platforms is not included
- All dates and timestamps are in UTC
- The source requires organization-level access (not available for individual accounts)

## Available Tables Summary

| Table | Incremental | Primary Key | Description |
|-------|-------------|-------------|-------------|
| `claude_code_usage` | ✅ | date, actor_type, actor_id, terminal_type | Daily Claude Code usage metrics |
| `usage_report` | Date Range | bucket, api_key_id, workspace_id, model, service_tier | API usage and latency metrics |
| `cost_report` | Date Range | bucket, workspace_id, description | Cost breakdown by workspace |
| `organization` | ❌ | - | Organization information |
| `workspaces` | ❌ | id | Workspace list |
| `api_keys` | ❌ | id | API key management |
| `invites` | ❌ | id | Pending invitations |
| `users` | ❌ | id | User list |
| `workspace_members` | ❌ | workspace_id, user_id | Workspace memberships |

For feature requests or issues, please create a GitHub issue at https://github.com/bruin-data/ingestr