# Fluxx

[Fluxx](https://www.fluxx.io/) is a cloud-based grants management platform designed to streamline and automate the entire grantmaking process for foundations, corporations, governments, and other funding organizations.

ingestr supports Fluxx as a source.

## URI format

The URI format for Fluxx is:

```plaintext
fluxx://<instance>?client_id=<client_id>&client_secret=<client_secret>
```

URI parameters:

- `instance`: Your Fluxx instance subdomain (e.g., `mycompany.preprod` for `https://mycompany.preprod.fluxxlabs.com`)
- `client_id`: OAuth 2.0 client ID for authentication
- `client_secret`: OAuth 2.0 client secret for authentication

## Example usage

### Basic usage - all fields

Assuming your instance is `myorg.preprod`, you can ingest grant requests into DuckDB using:

```bash
ingestr ingest \
--source-uri 'fluxx://myorg.preprod?client_id=your_client_id&client_secret=your_client_secret' \
--source-table 'grant_request' \
--dest-uri duckdb:///fluxx.duckdb \
--dest-table 'raw.grant_request'
```

### Custom field selection

You can select specific fields to ingest using the colon syntax:

```bash
ingestr ingest \
--source-uri 'fluxx://myorg.preprod?client_id=your_client_id&client_secret=your_client_secret' \
--source-table 'grant_request:id,amount_requested,amount_recommended,granted' \
--dest-uri duckdb:///fluxx.duckdb \
--dest-table 'raw.grant_request'
```

## Tables

Fluxx source currently supports the following 50 tables:

### Core Resources
- `claim_expense_datum`: Individual data entries within claim expense forms with budget category details
- `claim_expense_row`: Specific line items or rows within claim expense forms
- `claim_expense`: Claim expense forms and templates for financial tracking
- `claim`: Grant claims and payment requests
- `concept_initiative`: Concept initiatives linking programs, initiatives, and sub-programs/sub-initiatives
- `dashboard_theme`: Dashboard theme configurations for UI customization
- `etl_claim_expense_datum`: ETL data for claim expense items with comprehensive budget tracking details
- `etl_grantee_budget_tracker_actual`: ETL data for actual grantee budget tracker amounts and expenses
- `etl_grantee_budget_tracker_period_datum`: ETL data for grantee budget tracker period information with detailed financial tracking
- `etl_relationship`: ETL data for entity relationships tracking connections between users, organizations, requests, and other entities
- `etl_request_budget`: ETL budget data for request funding sources with comprehensive financial details
- `etl_request_transaction_budget`: ETL budget data for request transaction funding sources including payment tracking
- `exempt_organization`: Tax-exempt organization data including EIN, classification, and financial information
- `geo_city`: City geographic data with coordinates and postal codes
- `geo_county`: County geographic data with FIPS codes
- `geo_place`: Geographic places with ancestry and location data
- `geo_region`: Geographic regions
- `geo_state`: State geographic data with abbreviations and FIPS codes
- `grant_request`: Grant applications and requests (300+ fields)
- `grantee_budget_category`: Budget category definitions used by grantees for expense tracking
- `grantee_budget_tracker_period_datum_actual`: Actual expenses and amounts recorded for budget tracking periods
- `grantee_budget_tracker_period_datum`: Budget data entries for specific tracking periods
- `grantee_budget_tracker_period`: Time periods for budget tracking with start and end dates
- `grantee_budget_tracker_row`: Individual budget line items and categories within budget trackers
- `grantee_budget_tracker`: Budget tracking documents for grantee financial management
- `integration_log`: Integration and system logs for tracking data processing and errors
- `mac_model_type_dyn_financial_audit`: Dynamic financial audit models with audit tracking, compliance status, and financial variance analysis
- `mac_model_type_dyn_mel`: Dynamic Monitoring, Evaluation & Learning (MEL) models with performance indicators, baseline tracking, and evaluation metrics
- `mac_model_type_dyn_tool`: Dynamic tool management models for tracking deployment status, usage metrics, and tool effectiveness
- `machine_category`: Machine category definitions for workflow state management
- `model_attribute_value`: Model attribute values with hierarchical data and dependencies
- `model_document_sub_type`: Document sub-type definitions and categories
- `model_document_type`: Document type configurations including DocuSign integration and permissions
- `model_document`: Document metadata including file information, storage details, and document relationships
- `model_theme`: Model themes for categorization and program hierarchy organization
- `organization`: Organizations (grantees, fiscal sponsors, etc.)
- `population_estimate_year`: Yearly population estimates with income and demographic breakdowns
- `population_estimate`: Population estimates by geographic area with demographic data
- `program`: Funding programs and initiatives
- `request_report`: Reports submitted for grants
- `request_transaction_funding_source`: Funding source details for specific request transactions
- `request_transaction`: Financial transactions and payments
- `request_user`: Relationships between requests and users with roles and descriptions
- `salesforce_authentication`: Salesforce authentication configurations with OAuth tokens, connection management, and API usage tracking
- `sub_initiative`: Sub-initiatives for detailed planning
- `sub_program`: Sub-programs under main programs
- `ui_version`: User interface version information and system configuration
- `user_organization`: Relationships between users and organizations with roles, departments, and contact details
- `user`: User accounts and profiles

### Field Selection

Each resource contains numerous fields. You can:
1. **Ingest all fields**: Use the resource name directly (e.g., `grant_request`)
2. **Select specific fields**: Use colon syntax (e.g., `grant_request:id,name,amount_requested`)

The field selection feature is particularly useful for large resources like `grant_request` which has over 300 fields.

## Authentication

Fluxx uses OAuth 2.0 with client credentials flow. To obtain credentials:

1. Contact your Fluxx administrator to create an API client
2. You'll receive a `client_id` and `client_secret`
3. Note your Fluxx instance subdomain (the part before `.fluxxlabs.com`)
