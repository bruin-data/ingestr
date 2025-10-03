"""PlusVibeAI source settings and constants"""

# Default start date for PlusVibeAI API requests
DEFAULT_START_DATE = "2020-01-01"

# PlusVibeAI API request timeout in seconds
REQUEST_TIMEOUT = 300

# Default page size for paginated requests
DEFAULT_PAGE_SIZE = 100

# Maximum page size (adjust based on API limits)
MAX_PAGE_SIZE = 1000

# Base API path for PlusVibeAI
API_BASE_PATH = "/api/v1"

# Campaign fields to retrieve from PlusVibeAI API
CAMPAIGN_FIELDS = (
    # Basic Information
    "id",
    "camp_name",
    "parent_camp_id",
    "campaign_type",
    "organization_id",
    "workspace_id",
    "status",
    # Timestamps
    "created_at",
    "modified_at",
    "last_lead_sent",
    "last_paused_at_bounced",
    # Campaign Configuration
    "tags",
    "template_id",
    "email_accounts",
    "daily_limit",
    "interval_limit_in_min",
    "send_priority",
    "send_as_txt",
    # Tracking & Settings
    "is_emailopened_tracking",
    "is_unsubscribed_link",
    "exclude_ooo",
    "is_acc_based_sending",
    "send_risky_email",
    "unsub_blocklist",
    "other_email_acc",
    "is_esp_match",
    "stop_on_lead_replied",
    # Bounce Settings
    "is_pause_on_bouncerate",
    "bounce_rate_limit",
    "is_paused_at_bounced",
    # Schedule
    "schedule",
    "first_wait_time",
    "camp_st_date",
    "camp_end_date",
    # Events & Sequences
    "events",
    "sequences",
    "sequence_steps",
    "camp_emails",
    # Lead Statistics
    "lead_count",
    "completed_lead_count",
    "lead_contacted_count",
    # Email Performance Metrics
    "sent_count",
    "opened_count",
    "unique_opened_count",
    "replied_count",
    "bounced_count",
    "unsubscribed_count",
    # Reply Classification
    "positive_reply_count",
    "negative_reply_count",
    "neutral_reply_count",
    # Daily & Business Metrics
    "email_sent_today",
    "opportunity_val",
    "open_rate",
    "replied_rate",
    # Custom Data
    "custom_fields",
)

# Lead fields to retrieve from PlusVibeAI API
LEAD_FIELDS = (
    # Basic Information
    "_id",
    "organization_id",
    "campaign_id",
    "workspace_id",
    # Lead Status & Progress
    "is_completed",
    "current_step",
    "status",
    "label",
    # Email Account Info
    "email_account_id",
    "email_acc_name",
    # Campaign Info
    "camp_name",
    # Timestamps
    "created_at",
    "modified_at",
    "last_sent_at",
    # Email Engagement Metrics
    "sent_step",
    "replied_count",
    "opened_count",
    # Email Verification
    "is_mx",
    "mx",
    # Contact Information
    "email",
    "first_name",
    "last_name",
    "phone_number",
    # Address Information
    "address_line",
    "city",
    "state",
    "country",
    "country_code",
    # Professional Information
    "job_title",
    "department",
    "company_name",
    "company_website",
    "industry",
    # Social Media
    "linkedin_person_url",
    "linkedin_company_url",
    # Workflow
    "total_steps",
    # Bounce Information
    "bounce_msg",
)

# Email Account fields to retrieve from PlusVibeAI API
EMAIL_ACCOUNT_FIELDS = (
    # Basic Information
    "_id",
    "email",
    "status",
    "warmup_status",
    # Timestamps
    "timestamp_created",
    "timestamp_updated",
    # Payload - nested object containing all configuration
    "payload",
    # Payload sub-fields (for reference, stored in payload object):
    # - name (first_name, last_name)
    # - warmup (limit, warmup_custom_words, warmup_signature, advanced, increment, reply_rate)
    # - imap_host, imap_port
    # - smtp_host, smtp_port
    # - daily_limit, sending_gap
    # - reply_to, custom_domain, signature
    # - tags, cmps
    # - analytics (health_scores, reply_rates, daily_counters)
)

# Email fields to retrieve from PlusVibeAI API
EMAIL_FIELDS = (
    # Basic Information
    "id",
    "message_id",
    "is_unread",
    # Lead Information
    "lead",
    "lead_id",
    "campaign_id",
    # From Address
    "from_address_email",
    "from_address_json",
    # Subject & Content
    "subject",
    "content_preview",
    "body",
    # Headers & Metadata
    "headers",
    "label",
    "thread_id",
    "eaccount",
    # To/CC/BCC Addresses
    "to_address_email_list",
    "to_address_json",
    "cc_address_email_list",
    "cc_address_json",
    "bcc_address_email_list",
    # Timestamps
    "timestamp_created",
    "source_modified_at",
)

# Blocklist fields to retrieve from PlusVibeAI API
BLOCKLIST_FIELDS = (
    # Basic Information
    "_id",
    "workspace_id",
    "value",
    "created_by_label",
    # Timestamps
    "created_at",
)

# Webhook fields to retrieve from PlusVibeAI API
WEBHOOK_FIELDS = (
    # Basic Information
    "_id",
    "workspace_id",
    "org_id",
    "url",
    "name",
    "secret",
    # Configuration
    "camp_ids",
    "evt_types",
    "status",
    "integration_type",
    # Settings
    "ignore_ooo",
    "ignore_automatic",
    # Timestamps
    "created_at",
    "modified_at",
    "last_run",
    # Response Data
    "last_resp",
    "last_recv_resp",
    # User Information
    "created_by",
    "modified_by",
)

# Tag fields to retrieve from PlusVibeAI API
TAG_FIELDS = (
    # Basic Information
    "_id",
    "workspace_id",
    "org_id",
    "name",
    "color",
    "description",
    "status",
    # Timestamps
    "created_at",
    "modified_at",
)
