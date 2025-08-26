# İngestr Desteklenen Kaynak Tabloları

Bu dokümanda ingestr tarafından desteklenen tüm platformlar ve her platforma ait kaynak tabloları listelenmiştir.

## adjust

- `campaigns`: Retrieves data for a campaign, showing the app's revenue and network costs over multiple days.
- `creatives`: Retrieves data for a creative assets, detailing the app's revenue and network costs across multiple days.
- `events`: Retrieves data for events and event slugs.
- `custom`: Retrieves custom data based on the dimensions and metrics specified.

## airtable

- `<base_id>/<table_name>`: Airtable tabloları base ID ve tablo adı formatında kullanılır (örnek: `appXYZ/employee`).

## applovin

- `publisher-report`: Provides daily metrics from the report end point using the report_type publisher.
- `advertiser-report`: Provides daily metrics from the report end point using the report_type advertiser.
- `advertiser-probabilistic-report`: Provides daily metrics from the probabilisticReport end point using the report_type advertiser.
- `advertiser-ska-report`: Provides daily metrics from the skaReport end point using the report_type advertiser.
- `custom:{endpoint}:{report_type}:{columns}`: Custom reports with specified endpoint, report type, and columns.

## applovin_max

- `user_ad_revenue`: Provides daily metrics from the user level ad revenue API.

## appsflyer

- `campaigns`: Retrieves data for campaigns, detailing the app's costs, loyal users, total installs, and revenue over multiple days.
- `creatives`: Retrieves data for a creative asset, including revenue and cost.
- `custom:<dimensions>:<metrics>`: Retrieves data for custom tables, which can be specified by the user.

## appstore

- `app-downloads-detailed`: App download analytics including first-time downloads, redownloads, updates, and more.
- `app-store-discovery-and-engagement-detailed`: App Store discovery and engagement metrics including data about user engagement with your app's icons, product pages, in-app event pages, and other install sheets.
- `app-sessions-detailed`: App Session provides insights on how often people open your app, and how long they spend in your app.
- `app-store-installation-and-deletion-detailed`: App installation and deletion metrics including device to estimate the number of times people install and delete your App Store apps.
- `app-store-purchases-detailed`: App purchase analytics including revenue, payment methods, and content details.
- `app-crashes-expanded`: App crash analytics including crash counts, device information, and version details.

## asana

- `workspaces`: Information about people, materials, or assets required to complete a task or project successfully.
- `projects`: Collections of tasks and related information.
- `tasks`: Tasks within a project. Only tasks that belong to a project can be ingested.
- `tags`: Labels that can be attached to tasks, projects, or conversations to help categorize and organize them.
- `stories`: Updates or comments that team members can add to a task or project.
- `teams`: Groups of individuals who work together to complete projects and tasks.
- `users`: Individuals who have access to the Asana platform.

## athena

Athena is primarily used as a destination. No specific source tables defined as it works with SQL databases.

## attio

- `objects`: Objects are the data types used to store facts about your customers. Fetches all objects.
- `records:{object_api_slug}`: Fetches all records of an object. For example: `records:companies`.
- `lists`: Fetches all lists.
- `list_entries:{list_id}`: Lists all items in a specific list. For example: `list_entries:8abc-123-456-789d-123`.
- `all_list_entries:{object_api_slug}`: Fetches all the lists for an object, and then fetches all the entries from that list.

## bigquery

BigQuery is primarily used as both source and destination using SQL table names.

## chess

- `profiles`: Retrieves player profiles based on a list of player usernames.
- `games`: Retrieves players games for specified players.
- `archives`: Retrieves the URLs to game archives for specified players.

## clickhouse

ClickHouse is used as both source and destination using SQL table names.

## clickup

- `user`: The authorised user profile.
- `teams`: Workspaces available to the authenticated user.
- `spaces`: Spaces available within a workspace.
- `lists`: Lists contained in each space.
- `tasks`: Tasks belonging to each list.

## cratedb

CrateDB is used as both source and destination using SQL table names.

## csv

CSV files are used with file paths as table names.

## custom_queries

Custom SQL queries can be used with the format `query:<sql_query>`.

## databricks

Databricks is used as both source and destination using SQL table names.

## db2

IBM Db2 is used as source and destination using SQL table names.

## duckdb

DuckDB is used as both source and destination using SQL table names.

## dynamodb

DynamoDB tables are specified by their table names.

## elasticsearch

- `<index-name>`: Fetches all available documents from the specified index.

## facebook-ads

- `campaigns`: Retrieves campaign data with various fields.
- `ad_sets`: Retrieves ad set data with various fields.
- `ads`: Retrieves ad data with various fields.
- `ad_creatives`: Retrieves ad creative data with various fields.
- `leads`: Retrieves lead data with various fields.
- `facebook_insights`: Retrieves insights data with configurable dimensions and metrics.

## fluxx

- `claim_expense_datum`: Individual data entries within claim expense forms.
- `claim_expense_row`: Specific line items or rows within claim expense forms.
- `claim_expense`: Claim expense forms and templates for financial tracking.
- `claim`: Grant claims and payment requests.
- `concept_initiative`: Concept initiatives linking programs, initiatives, and sub-programs.
- `dashboard_theme`: Dashboard theme configurations for UI customization.
- `etl_claim_expense_datum`: ETL data for claim expense items.
- `etl_grantee_budget_tracker_actual`: ETL data for actual grantee budget tracker amounts.
- `etl_grantee_budget_tracker_period_datum`: ETL data for grantee budget tracker period information.
- `etl_relationship`: ETL data for entity relationships.
- `etl_request_budget`: ETL budget data for request funding sources.
- `etl_request_transaction_budget`: ETL budget data for request transaction funding sources.
- `exempt_organization`: Tax-exempt organization data.
- `geo_city`: City geographic data with coordinates and postal codes.
- `geo_county`: County geographic data with FIPS codes.
- `geo_place`: Geographic places with ancestry and location data.
- `geo_region`: Geographic regions.
- `geo_state`: State geographic data with abbreviations and FIPS codes.
- `grant_request`: Grant applications and requests (300+ fields).
- `grantee_budget_category`: Budget category definitions used by grantees.
- `grantee_budget_tracker_period_datum_actual`: Actual expenses and amounts recorded.
- `grantee_budget_tracker_period_datum`: Budget data entries for specific tracking periods.
- `grantee_budget_tracker_period`: Time periods for budget tracking.
- `grantee_budget_tracker_row`: Individual budget line items and categories.
- `grantee_budget_tracker`: Budget tracking documents for grantee financial management.
- `integration_log`: Integration and system logs.
- `mac_model_type_dyn_financial_audit`: Dynamic financial audit models.
- `mac_model_type_dyn_mel`: Dynamic Monitoring, Evaluation & Learning models.
- `mac_model_type_dyn_tool`: Dynamic tool management models.
- `machine_category`: Machine category definitions for workflow state management.
- `model_attribute_value`: Model attribute values with hierarchical data.
- `model_document_sub_type`: Document sub-type definitions and categories.
- `model_document_type`: Document type configurations.
- `model_document`: Document metadata including file information.
- `model_theme`: Model themes for categorization.
- `organization`: Organizations (grantees, fiscal sponsors, etc.).
- `population_estimate_year`: Yearly population estimates.
- `population_estimate`: Population estimates by geographic area.
- `program`: Funding programs and initiatives.
- `request_report`: Reports submitted for grants.
- `request_transaction_funding_source`: Funding source details.
- `request_transaction`: Financial transactions and payments.
- `request_user`: Relationships between requests and users.
- `salesforce_authentication`: Salesforce authentication configurations.
- `sub_initiative`: Sub-initiatives for detailed planning.
- `sub_program`: Sub-programs under main programs.
- `ui_version`: User interface version information.
- `user_organization`: Relationships between users and organizations.
- `user`: User accounts and profiles.

## frankfurter

- `currencies`: Retrieves list of available currencies with ISO 4217 codes and names.
- `latest`: Fetches latest exchange rates for all currencies.
- `exchange_rates`: Retrieves historical exchange rates for specified date range.

## freshdesk

- `agents`: Retrieves users responsible for managing and resolving customer inquiries.
- `companies`: Retrieves customer organizations or groups that agents support.
- `contacts`: Retrieves individuals or customers who reach out for support.
- `groups`: Retrieves agents organized based on specific criteria.
- `roles`: Retrieves predefined sets of permissions.
- `tickets`: Retrieves customer inquiries or issues submitted via various channels.

## gcs

Google Cloud Storage files are specified using bucket and file path format: `{bucket name}/{file glob}`.

## github

- `issues`: Retrieves GitHub issues along with their comments and reactions.
- `pull_requests`: Retrieves pull requests with comments and reactions.
- `repo_events`: Retrieves recent repository events.
- `stargazers`: Retrieves stargazers.

## google-ads

- `account_report_daily`: Provides daily metrics aggregated at the account level.
- `campaign_report_daily`: Provides daily metrics aggregated at the campaign level.
- `ad_group_report_daily`: Provides daily metrics aggregated at the ad group level.
- `ad_report_daily`: Provides daily metrics aggregated at the ad level.
- `audience_report_daily`: Provides daily metrics aggregated at the audience level.
- `keyword_report_daily`: Provides daily metrics aggregated at the keyword level.
- `click_report_daily`: Provides daily metrics on clicks.
- `landing_page_report_daily`: Provides daily metrics on landing page performance.
- `search_keyword_report_daily`: Provides daily metrics on search keywords.
- `search_term_report_daily`: Provides daily metrics on search terms.
- `lead_form_submission_data_report_daily`: Provides daily metrics on lead form submissions.
- `local_services_lead_report_daily`: Provides daily metrics on local services leads.
- `local_services_lead_conversations_report_daily`: Provides daily metrics on local services lead conversations.
- `daily:{resource_name}:{dimensions}:{metrics}`: Custom reports with specified resource, dimensions, and metrics.

## google_analytics

- `realtime:<dimensions>:<metrics>:[<minutes_ranges>]`: Retrieves real-time analytics data.
- `custom:<dimensions>:<metrics>`: Retrieves custom reports based on specified dimensions and metrics.

## gorgias

- `customers`: Customers are the users who have interacted with the support team.
- `tickets`: Tickets are the main entity in Gorgias, representing customer inquiries.
- `ticket_messages`: Ticket messages are the messages exchanged between the customer and the support agent.
- `satisfaction_surveys`: Satisfaction surveys are sent to customers after a ticket is resolved.

## gsheets

- `<spreadsheet_id>.<sheet_name>`: Google Sheets tabloları spreadsheet ID ve sheet adı formatında kullanılır.

## hubspot

- `companies`: Retrieves information about organizations.
- `contacts`: Retrieves information about visitors, potential customers, and leads.
- `deals`: Retrieves deal records and tracks deal progress.
- `tickets`: Handles requests for help from customers or users.
- `products`: Retrieves pricing information of products.
- `quotes`: Retrieves price proposals that salespeople can create and send.
- `schemas`: Returns all object schemas that have been defined for your account.

## influxdb

InfluxDB accepts any measurement name as the source table.

## isoc-pulse

- `dnssec_adoption`: DNSSEC adoption metrics for specific domains.
- `dnssec_tld_adoption`: DNSSEC adoption metrics for top-level domains.
- `dnssec_validation`: DNSSEC validation metrics.
- `http`: HTTP protocol metrics.
- `http3`: HTTP/3 protocol metrics.
- `https`: HTTPS adoption metrics.
- `ipv6`: IPv6 adoption metrics.
- `net_loss`: Internet disconnection metrics.
- `resilience`: Internet resilience metrics.
- `roa`: Route Origin Authorization metrics.
- `rov`: Route Origin Validation metrics.
- `tls`: TLS protocol metrics.
- `tls13`: TLS 1.3 protocol metrics.

## kafka

Kafka topics are specified by their topic names.

## kinesis

Kinesis streams are specified by their stream names or ARNs.

## klaviyo

- `events`: Retrieves all events in an account.
- `profiles`: Retrieves all profiles in an account.
- `campaigns`: Retrieves all campaigns in an account.
- `metrics`: Retrieves all metrics in an account.
- `tags`: Retrieves all tags in an account.
- `coupons`: Retrieves all coupons in an account.
- `catalog-variants`: Retrieves all variants in an account.
- `catalog-categories`: Retrieves all catalog categories in an account.
- `catalog-items`: Retrieves all catalog items in an account.
- `flows`: Retrieves all flows in an account.
- `lists`: Retrieves all lists in an account.
- `images`: Retrieves all images in an account.
- `segments`: Retrieves all segments in an account.
- `forms`: Retrieves all forms in an account.
- `templates`: Retrieves all templates in an account.

## linear

- `issues`: Fetches all issues from your Linear workspace.
- `users`: Fetches users from your workspace.
- `workflow_states`: Fetches workflow states used in your Linear workspace.
- `cycles`: Fetches cycle information and planning data.
- `attachments`: Fetches file attachments associated with issues.
- `comments`: Fetches comments on issues and other entities.
- `documents`: Fetches documents created in Linear.
- `external_users`: Fetches information about external users.
- `initiative`: Fetches initiative data for high-level planning.
- `integrations`: Fetches integration configurations.
- `labels`: Fetches labels used for categorizing issues.
- `project_updates`: Fetches updates posted to projects.
- `team_memberships`: Fetches team membership information.
- `initiative_to_project`: Fetches relationships between initiatives and projects.
- `project_milestone`: Retrieves Linear project milestones and checkpoints.
- `project_status`: Fetches project status information.
- `projects`: Fetches project-level data.
- `teams`: Fetches information about the teams configured in Linear.
- `organization`: Fetches organization-level information.

## linkedin_ads

- `custom:<dimensions>:<metrics>`: Custom reports allow you to retrieve data based on specific dimensions and metrics.

## mixpanel

- `events`: Retrieves events data.
- `profiles`: Retrieves Mixpanel user profiles and attributes.

## mongodb

MongoDB collections are specified in `database.collection` format or with custom aggregation pipelines using `database.collection:[aggregation_pipeline]` format.

## motherduck

MotherDuck is used as both source and destination using SQL table names.

## mssql

Microsoft SQL Server is used as both source and destination using SQL table names.

## notion

Notion databases are specified by their database IDs.

## personio

- `employees`: Retrieves company employees details.
- `absence_types`: Retrieves list of various types of employee absences.
- `absences`: Fetches absence periods for absences with time unit set to days.
- `attendances`: Retrieves attendance records for each employee.
- `projects`: Retrieves a list of all company projects.
- `document_categories`: Retrieves all document categories of the company.
- `custom_reports_list`: Retrieves metadata about existing custom reports.
- `employees_absences_balance`: Retrieves the absence balance for a specific employee.

## postgres

PostgreSQL is used as both source and destination using SQL table names.

## revenuecat

- `projects`: Fetches all projects from your RevenueCat account.
- `customers`: Fetches all customers with nested purchases and subscriptions data.
- `products`: Fetches all products configured in your RevenueCat project.
- `entitlements`: Fetches all entitlements configured in your RevenueCat project.
- `offerings`: Fetches all offerings configured in your RevenueCat project.

## salesforce

- `user`: Refers to an individual who has access to a Salesforce org or instance.
- `user_role`: A standard object that represents a role within the organization's hierarchy.
- `opportunity`: Represents a sales opportunity for a specific account or contact.
- `opportunity_line_item`: Represents individual line items or products associated with an Opportunity.
- `opportunity_contact_role`: Represents the association between an Opportunity and a Contact.
- `account`: Individual or organization that interacts with your business.
- `contact`: An individual person associated with an account or organization.
- `lead`: Prospective customer/individual/org. that has shown interest in a company's products/services.
- `campaign`: Marketing initiative or project designed to achieve specific goals.
- `campaign_member`: Association between a Contact or Lead and a Campaign.
- `product`: For managing and organizing your product-related data.
- `pricebook`: Used to manage product pricing and create price books.
- `pricebook_entry`: Represents a specific price for a product in a price book.
- `task`: Used to track and manage various activities and tasks.
- `event`: Used to track and manage calendar-based events.

## shopify

- `orders`: Retrieves Shopify order data including customer info, line items, and shipping details.
- `customers`: Retrieves Shopify customer data including contact info and order history.
- `discounts`: Retrieves Shopify discount data using GraphQL API.
- `products`: Retrieves Shopify product information including variants, images, and inventory.
- `inventory_items`: Retrieves Shopify inventory item details and stock levels.
- `transactions`: Retrieves Shopify transaction data for payments and refunds.
- `balance`: Retrieves Shopify balance information for financial tracking.
- `events`: Retrieves Shopify event data for audit trails and activity tracking.
- `price_rules`: **DEPRECATED** - Use `discounts` table instead.

## stripe

- `account`: Contains information about a Stripe account.
- `apple_pay_domain`: Represents Apple Pay domains registered with Stripe.
- `application_fee`: Records fees collected by platforms.
- `checkout_session`: Contains data about Checkout sessions.
- `coupon`: Stores data about discount codes or coupons.
- `customer`: Holds information about customers.
- `dispute`: Records payment disputes and chargebacks.
- `payment_intent`: Represents payment intents tracking the lifecycle of payments.
- `payment_link`: Contains information about payment links.
- `payment_method`: Stores payment method information.
- `payment_method_domain`: Represents domains verified for payment method collection.
- `payout`: Records payouts made from Stripe accounts.
- `plan`: Contains subscription plan information.
- `price`: Contains pricing information for products.
- `product`: Represents products that can be sold or subscribed to.
- `promotion_code`: Stores data about promotion codes.
- `quote`: Contains quote information for customers.
- `refund`: Records refunds issued for charges.
- `review`: Contains payment review information.
- `setup_attempt`: Records attempts to set up payment methods.
- `setup_intent`: Represents setup intents for collecting payment method information.
- `shipping_rate`: Contains shipping rate information.
- `subscription`: Represents a customer's subscription to a recurring service.
- `subscription_item`: Contains individual items within a subscription.
- `subscription_schedule`: Represents scheduled changes to subscriptions.
- `tax_code`: Contains tax code information.
- `tax_id`: Stores tax ID information.
- `tax_rate`: Contains tax rate information.
- `top_up`: Records top-ups made to Stripe accounts.
- `transfer`: Records transfers between Stripe accounts.
- `webhook_endpoint`: Contains webhook endpoint configurations.
- `balance_transaction`: Records transactions that affect the Stripe account balance.
- `charge`: Returns a list of charges.
- `credit_note`: Contains credit note information.
- `event`: Logs all events in the Stripe account.
- `invoice`: Represents invoices sent to customers.
- `invoice_item`: Contains individual line items that can be added to invoices.
- `invoice_line_item`: Represents line items within invoices.

**Not:** Stripe tabloları farklı loading modlarını destekler:
- `<endpoint>`: Standard async loading
- `<endpoint>:sync`: Full loading with synchronous processing
- `<endpoint>:sync:incremental`: Incremental loading mode

---

Bu dokümanda ingestr tarafından desteklenen tüm platformlar ve kaynak tabloları yer almaktadır. Her platform için detaylı kullanım örnekleri ve parametre bilgileri için ilgili platform dokümantasyonuna bakabilirsiniz.