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

Fluxx source allows ingesting the following sources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| `adhoc_report` | id | - | replace | Ad-hoc reports with SQL queries, filters, columns configuration, and report metadata |
| `affiliate` | id | - | replace | User affiliations with organizations including contact information, roles, and membership details |
| `claim` | id | - | replace | Grant claims and payment requests with approval status, amounts, and dates |
| `claim_expense` | id | - | replace | Claim expense forms and templates for financial tracking |
| `claim_expense_datum` | id | - | replace | Individual data entries within claim expense forms with budget category details |
| `claim_expense_row` | id | - | replace | Specific line items or rows within claim expense forms |
| `concept_initiative` | id | - | replace | Concept initiatives linking programs, initiatives, and sub-programs/sub-initiatives |
| `dashboard_theme` | id | - | replace | Dashboard theme configurations for UI customization |
| `etl_claim_expense_datum` | id | - | replace | ETL data for claim expense items with comprehensive budget tracking details |
| `etl_grantee_budget_tracker_actual` | id | - | replace | ETL data for actual grantee budget tracker amounts and expenses |
| `etl_grantee_budget_tracker_period_datum` | id | - | replace | ETL data for grantee budget tracker period information with detailed financial tracking |
| `etl_relationship` | id | - | replace | ETL data for entity relationships tracking connections between users, organizations, requests, and other entities |
| `etl_request_budget` | id | - | replace | ETL budget data for request funding sources with comprehensive financial details |
| `etl_request_transaction_budget` | id | - | replace | ETL budget data for request transaction funding sources including payment tracking |
| `exempt_organization` | id | - | replace | Tax-exempt organization data including EIN, classification, and financial information |
| `geo_city` | id | - | replace | City geographic data with coordinates and postal codes |
| `geo_county` | id | - | replace | County geographic data with FIPS codes |
| `geo_place` | id | - | replace | Geographic places with ancestry and location data |
| `geo_region` | id | - | replace | Geographic regions |
| `geo_state` | id | - | replace | State geographic data with abbreviations and FIPS codes |
| `grant_request` | id | - | replace | Grant applications and requests with comprehensive details (300+ fields) |
| `grantee_budget_category` | id | - | replace | Budget category definitions used by grantees for expense tracking |
| `grantee_budget_tracker` | id | - | replace | Budget tracking documents for grantee financial management |
| `grantee_budget_tracker_period` | id | - | replace | Time periods for budget tracking with start and end dates |
| `grantee_budget_tracker_period_datum` | id | - | replace | Budget data entries for specific tracking periods |
| `grantee_budget_tracker_period_datum_actual` | id | - | replace | Actual expenses and amounts recorded for budget tracking periods |
| `grantee_budget_tracker_row` | id | - | replace | Individual budget line items and categories within budget trackers |
| `integration_log` | id | - | replace | Integration and system logs for tracking data processing and errors |
| `mac_model_type_dyn_financial_audit` | id | - | replace | Dynamic financial audit models with audit tracking, compliance status, and financial variance analysis |
| `mac_model_type_dyn_mel` | id | - | replace | Dynamic Monitoring, Evaluation & Learning (MEL) models with performance indicators, baseline tracking, and evaluation metrics |
| `mac_model_type_dyn_tool` | id | - | replace | Dynamic tool management models for tracking deployment status, usage metrics, and tool effectiveness |
| `machine_category` | id | - | replace | Machine category definitions for workflow state management |
| `model_attribute_value` | id | - | replace | Model attribute values with hierarchical data and dependencies |
| `model_document` | id | - | replace | Document metadata including file information, storage details, and document relationships |
| `model_document_sub_type` | id | - | replace | Document sub-type definitions and categories |
| `model_document_type` | id | - | replace | Document type configurations including DocuSign integration and permissions |
| `model_theme` | id | - | replace | Model themes for categorization and program hierarchy organization |
| `organization` | id | - | replace | Organizations (grantees, fiscal sponsors, etc.) with contact information and tax details |
| `population_estimate` | id | - | replace | Population estimates by geographic area with demographic data |
| `population_estimate_year` | id | - | replace | Yearly population estimates with income and demographic breakdowns |
| `program` | id | - | replace | Funding programs and initiatives |
| `request_report` | id | - | replace | Reports submitted for grants |
| `request_transaction` | id | - | replace | Financial transactions and payments |
| `request_transaction_funding_source` | id | - | replace | Funding source details for specific request transactions |
| `request_user` | id | - | replace | Relationships between requests and users with roles and descriptions |
| `salesforce_authentication` | id | - | replace | Salesforce authentication configurations with OAuth tokens, connection management, and API usage tracking |
| `initiative` | id | - | replace | Third level of program hierarchy describing high-level goals of philanthropy efforts |
| `sub_initiative` | id | - | replace | Sub-initiatives for detailed planning |
| `sub_program` | id | - | replace | Sub-programs under main programs |
| `ui_version` | id | - | replace | User interface version information and system configuration |
| `user` | id | - | replace | User accounts and profiles |
| `user_organization` | id | - | replace | Relationships between users and organizations with roles, departments, and contact details |
| `affiliate_type` | id | - | replace | Affiliate Type management and tracking |
| `aha_requirements_tickets` | id | - | replace | Aha Requirements Tickets management and tracking |
| `alert` | id | - | replace | Alert management and tracking |
| `alert_email` | id | - | replace | Alert email configurations and templates for notification delivery |
| `alert_email_user` | id | - | replace | Alert Email User management and tracking |
| `alert_model_log` | id | - | replace | Alert Model Log management and tracking |
| `alert_recipient` | id | - | replace | Alert Recipient management and tracking |
| `alert_transition_state` | id | - | replace | Alert Transition State management and tracking |
| `bank_account` | id | - | replace | Bank Account management and tracking |
| `budget_request` | id | - | replace | Budget Request management and tracking |
| `card_configuration` | id | - | replace | Card Configuration management and tracking |
| `census_code_result` | id | - | replace | Census Code Result management and tracking |
| `census_config` | id | - | replace | Census Config management and tracking |
| `claim_expense_row_document` | id | - | replace | Claim Expense Row Document management and tracking |
| `clean_calculation` | id | - | replace | Clean Calculation management and tracking |
| `client_configuration` | id | - | replace | Client Configuration management and tracking |
| `client_store` | id | - | replace | Client Store management and tracking |
| `client_store_dashboard_group` | id | - | replace | Client Store Dashboard Group management and tracking |
| `clone_ancestry` | id | - | replace | Clone Ancestry management and tracking |
| `code_block_conversion` | id | - | replace | Code Block Conversion management and tracking |
| `coi` | id | - | replace | Coi management and tracking |
| `compliance_checklist_item` | id | - | replace | Compliance Checklist Item management and tracking |
| `config_model_document` | id | - | replace | Config Model Document management and tracking |
| `configuration_value` | id | - | replace | Configuration Value management and tracking |
| `cpi` | id | - | replace | Cpi management and tracking |
| `dashboard_group` | id | - | replace | Dashboard Group management and tracking |
| `dashboard_template` | id | - | replace | Dashboard Template management and tracking |
| `demographic_category` | id | - | replace | Demographic Category management and tracking |
| `demographic_item` | id | - | replace | Demographic Item management and tracking |
| `document` | id | - | replace | Document management and tracking |
| `docusign_token` | id | - | replace | Docusign Token management and tracking |
| `email_user` | id | - | replace | Email User management and tracking |
| `expirable_token` | id | - | replace | Expirable Token management and tracking |
| `extract_format` | id | - | replace | Extract Format management and tracking |
| `favorite` | id | - | replace | Favorite management and tracking |
| `field_list` | id | - | replace | Field List management and tracking |
| `filter` | id | - | replace | Filter management and tracking |
| `form` | id | - | replace | Form management and tracking |
| `form_element` | id | - | replace | Form Element management and tracking |
| `fund` | id | - | replace | Fund management and tracking |
| `fund_docket` | id | - | replace | Fund Docket management and tracking |
| `fund_line_item` | id | - | replace | Fund Line Item management and tracking |
| `funding_source` | id | - | replace | Funding Source management and tracking |
| `funding_source_allocation` | id | - | replace | Funding Source Allocation management and tracking |
| `funding_source_allocation_authority` | id | - | replace | Funding Source Allocation Authority management and tracking |
| `funding_source_forecast` | id | - | replace | Funding Source Forecast management and tracking |
| `fx_conversion` | id | - | replace | Fx Conversion management and tracking |
| `fx_type` | id | - | replace | Fx Type management and tracking |
| `generic_template` | id | - | replace | Generic Template management and tracking |
| `geo_country` | id | - | replace | Geo Country management and tracking |
| `geo_place_relationship` | id | - | replace | Geo Place Relationship management and tracking |
| `grant_outcome` | id | - | replace | Grant Outcome management and tracking |
| `grant_outcome_progress_report` | id | - | replace | Grant Outcome Progress Report management and tracking |
| `grant_outcome_progress_report_row` | id | - | replace | Grant Outcome Progress Report Row management and tracking |
| `grant_outcome_row` | id | - | replace | Grant Outcome Row management and tracking |
| `grant_output` | id | - | replace | Grant Output management and tracking |
| `grant_output_period` | id | - | replace | Grant Output Period management and tracking |
| `grant_output_period_datum` | id | - | replace | Grant Output Period Datum management and tracking |
| `grant_output_progress_report` | id | - | replace | Grant Output Progress Report management and tracking |
| `grant_output_progress_report_datum` | id | - | replace | Grant Output Progress Report Datum management and tracking |
| `grant_output_row` | id | - | replace | Grant Output Row management and tracking |
| `grantee_budget` | id | - | replace | Grantee Budget management and tracking |
| `grantee_budget_category_group` | id | - | replace | Grantee Budget Category Group management and tracking |
| `grantee_budget_category_group_relationship` | id | - | replace | Grantee Budget Category Group Relationship management and tracking |
| `grantee_budget_tracker_period_amendment` | id | - | replace | Grantee Budget Tracker Period Amendment management and tracking |
| `grantee_whitelist` | id | - | replace | Grantee Whitelist management and tracking |
| `group` | id | - | replace | Group management and tracking |
| `group_member` | id | - | replace | Group Member management and tracking |
| `gs_stream` | id | - | replace | Gs Stream management and tracking |
| `gs_stream_document` | id | - | replace | Gs Stream Document management and tracking |
| `gs_stream_gs_tag` | id | - | replace | Gs Stream Gs Tag management and tracking |
| `gs_stream_request` | id | - | replace | Gs Stream Request management and tracking |
| `gs_tag` | id | - | replace | Gs Tag management and tracking |
| `indicator` | id | - | replace | Indicator management and tracking |
| `integration_filter` | id | - | replace | Integration Filter management and tracking |
| `job` | id | - | replace | Job management and tracking |
| `language` | id | - | replace | Language management and tracking |
| `login_attempt` | id | - | replace | Login Attempt management and tracking |
| `loi` | id | - | replace | Loi management and tracking |
| `machine_event` | id | - | replace | Machine Event management and tracking |
| `machine_event_from_state` | id | - | replace | Machine Event From State management and tracking |
| `machine_event_role` | id | - | replace | Machine Event Role management and tracking |
| `machine_model_type` | id | - | replace | Machine Model Type management and tracking |
| `machine_state` | id | - | replace | Machine State management and tracking |
| `machine_state_category` | id | - | replace | Machine State Category management and tracking |
| `machine_state_group` | id | - | replace | Machine State Group management and tracking |
| `machine_workflow` | id | - | replace | Machine Workflow management and tracking |
| `machine_workflow_fork` | id | - | replace | Machine Workflow Fork management and tracking |
| `matching_gift_profile` | id | - | replace | Matching Gift Profile management and tracking |
| `mention` | id | - | replace | Mention management and tracking |
| `migrate_row` | id | - | replace | Migrate Row management and tracking |
| `migration` | id | - | replace | Migration management and tracking |
| `migration_config` | id | - | replace | Migration Config management and tracking |
| `migration_config_column` | id | - | replace | Migration Config Column management and tracking |
| `migration_config_model` | id | - | replace | Migration Config Model management and tracking |
| `migration_config_model_link` | id | - | replace | Migration Config Model Link management and tracking |
| `migration_file` | id | - | replace | Migration File management and tracking |
| `model_attribute` | id | - | replace | Model Attribute management and tracking |
| `model_attribute_choice` | id | - | replace | Model Attribute Choice management and tracking |
| `model_clone_configuration` | id | - | replace | Model Clone Configuration management and tracking |
| `model_document_dynamic_recipient` | id | - | replace | Model Document Dynamic Recipient management and tracking |
| `model_document_master` | id | - | replace | Model Document Master management and tracking |
| `model_document_sign` | id | - | replace | Model Document Sign management and tracking |
| `model_document_sign_envelope` | id | - | replace | Model Document Sign Envelope management and tracking |
| `model_document_template` | id | - | replace | Model Document Template management and tracking |
| `model_email` | id | - | replace | Model Email management and tracking |
| `model_method` | id | - | replace | Model Method management and tracking |
| `model_summary` | id | - | replace | Model Summary management and tracking |
| `model_validation` | id | - | replace | Model Validation management and tracking |
| `model_validation_field` | id | - | replace | Model Validation Field management and tracking |
| `modification` | id | - | replace | Modification management and tracking |
| `multi_element_choice` | id | - | replace | Multi Element Choice management and tracking |
| `multi_element_group` | id | - | replace | Multi Element Group management and tracking |
| `multi_element_value` | id | - | replace | Multi Element Value management and tracking |
| `note` | id | - | replace | Note management and tracking |
| `ofac_person` | id | - | replace | Ofac Person management and tracking |
| `organization_connection_request` | id | - | replace | Organization Connection Request management and tracking |
| `outcome` | id | - | replace | Outcome management and tracking |
| `periodic_sync` | id | - | replace | Periodic Sync management and tracking |
| `perishable_token` | id | - | replace | Perishable Token management and tracking |
| `permission_delegator` | id | - | replace | Permission Delegator management and tracking |
| `persona` | id | - | replace | Persona management and tracking |
| `post` | id | - | replace | Post management and tracking |
| `post_relationship` | id | - | replace | Post Relationship management and tracking |
| `post_view` | id | - | replace | Post View management and tracking |
| `primary_contact_tenure` | id | - | replace | Primary Contact Tenure management and tracking |
| `program_budget` | id | - | replace | Program Budget management and tracking |
| `project` | id | - | replace | Project management and tracking |
| `project_list` | id | - | replace | Project List management and tracking |
| `project_list_item` | id | - | replace | Project List Item management and tracking |
| `project_organization` | id | - | replace | Project Organization management and tracking |
| `project_request` | id | - | replace | Project Request management and tracking |
| `project_user` | id | - | replace | Project User management and tracking |
| `real_me_invitation` | id | - | replace | Real Me Invitation management and tracking |
| `realtime_update` | id | - | replace | Realtime Update management and tracking |
| `recommendation_email` | id | - | replace | Recommendation Email management and tracking |
| `reduce_indexing_record` | id | - | replace | Reduce Indexing Record management and tracking |
| `relationship` | id | - | replace | Relationship management and tracking |
| `relationship_schema_mapping` | id | - | replace | Relationship Schema Mapping management and tracking |
| `request_amendment` | id | - | replace | Request Amendment management and tracking |
| `request_amendment_model_themes` | id | - | replace | Request Amendment Model Themes management and tracking |
| `request_evaluation_metric` | id | - | replace | Request Evaluation Metric management and tracking |
| `request_funding_source` | id | - | replace | Request Funding Source management and tracking |
| `request_geo_state` | id | - | replace | Request Geo State management and tracking |
| `request_organization` | id | - | replace | Request Organization management and tracking |
| `request_outcome` | id | - | replace | Request Outcome management and tracking |
| `request_program` | id | - | replace | Request Program management and tracking |
| `request_recommendation` | id | - | replace | Request Recommendation management and tracking |
| `request_recommender` | id | - | replace | Request Recommender management and tracking |
| `request_regrant` | id | - | replace | Request Regrant management and tracking |
| `request_review` | id | - | replace | Request Review management and tracking |
| `request_review_set` | id | - | replace | Request Review Set management and tracking |
| `request_reviewer_assignment` | id | - | replace | Request Reviewer Assignment management and tracking |
| `role` | id | - | replace | Role management and tracking |
| `role_user` | id | - | replace | Role User management and tracking |
| `section` | id | - | replace | Section management and tracking |
| `segment` | id | - | replace | Segment management and tracking |
| `segment_tag` | id | - | replace | Segment Tag management and tracking |
| `shared_card` | id | - | replace | Shared Card management and tracking |
| `sms_log` | id | - | replace | Sms Log management and tracking |
| `spending_forecast` | id | - | replace | Spending Forecast management and tracking |
| `sphinx_check` | id | - | replace | Sphinx Check management and tracking |
| `stencil` | id | - | replace | Stencil management and tracking |
| `stencil_book` | id | - | replace | Stencil Book management and tracking |
| `stencil_book_page` | id | - | replace | Stencil Book Page management and tracking |
| `stencil_form` | id | - | replace | Stencil Form management and tracking |
| `sub_model` | id | - | replace | Sub Model management and tracking |
| `table_view` | id | - | replace | Table View management and tracking |
| `table_view_favorite` | id | - | replace | Table View Favorite management and tracking |
| `tag` | id | - | replace | Tag management and tracking |
| `tagging` | id | - | replace | Tagging management and tracking |
| `transaction_report_dependency` | id | - | replace | Transaction Report Dependency management and tracking |
| `translator_assignment` | id | - | replace | Translator Assignment management and tracking |
| `translator_language` | id | - | replace | Translator Language management and tracking |
| `user_email` | id | - | replace | User Email management and tracking |
| `user_permission` | id | - | replace | User Permission management and tracking |
| `user_profile` | id | - | replace | User Profile management and tracking |
| `user_profile_rule` | id | - | replace | User Profile Rule management and tracking |
| `user_segment_tag` | id | - | replace | User Segment Tag management and tracking |
| `webhook_subscription` | id | - | replace | Webhook Subscription management and tracking |
| `wiki_document` | id | - | replace | Wiki Document management and tracking |
| `wiki_document_template` | id | - | replace | Wiki Document Template management and tracking |
| `work_task` | id | - | replace | Work Task management and tracking |
| `workflow_event` | id | - | replace | Workflow Event management and tracking |
| `zenith_user_configuration` | id | - | replace | Zenith User Configuration management and tracking |

Use these as `--source-table` parameter in the `ingestr ingest` command.

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
