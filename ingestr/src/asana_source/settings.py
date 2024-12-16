"""Asana source settings and constants"""

# Default start date for Asana API requests, only tasks started after this date will be collected
DEFAULT_START_DATE = "2010-01-01T00:00:00.000Z"

# Asana API request timeout
REQUEST_TIMEOUT = 300

# list of workspace fields to be retrieved from Asana API
WORKSPACE_FIELDS = ("gid", "name", "is_organization", "resource_type", "email_domains")

# List of project fields to be retrieved from Asana API
PROJECT_FIELDS = (
    "name",
    "gid",
    "owner",
    "current_status",
    "custom_fields",
    "default_view",
    "due_date",
    "due_on",
    "is_template",
    "created_at",
    "modified_at",
    "start_on",
    "archived",
    "public",
    "members",
    "followers",
    "color",
    "notes",
    "icon",
    "permalink_url",
    "workspace",
    "team",
    "resource_type",
    "current_status_update",
    "custom_field_settings",
    "completed",
    "completed_at",
    "completed_by",
    "created_from_template",
    "project_brief",
)

# List of section fields to be retrieved from Asana API
SECTION_FIELDS = (
    "gid",
    "resource_type",
    "name",
    "created_at",
    "project",
    "projects",
)

# List of tag fields to be retrieved from Asana API
TAG_FIELDS = (
    "gid",
    "resource_type",
    "created_at",
    "followers",
    "name",
    "color",
    "notes",
    "permalink_url",
    "workspace",
)

# List of task fields to be retrieved from Asana API
TASK_FIELDS = (
    "gid",
    "resource_type",
    "name",
    "approval_status",
    "assignee_status",
    "created_at",
    "assignee",
    "start_on",
    "start_at",
    "due_on",
    "due_at",
    "completed",
    "completed_at",
    "completed_by",
    "modified_at",
    "dependencies",
    "dependents",
    "external",
    "notes",
    "num_subtasks",
    "resource_subtype",
    "followers",
    "parent",
    "permalink_url",
    "tags",
    "workspace",
    "custom_fields",
    "project",
    "memberships",
    "memberships.project.name",
    "memberships.section.name",
)

# List of story fields to be retrieved from Asana API
STORY_FIELDS = (
    "gid",
    "resource_type",
    "created_at",
    "created_by",
    "resource_subtype",
    "text",
    "is_pinned",
    "assignee",
    "dependency",
    "follower",
    "new_section",
    "old_section",
    "new_text_value",
    "old_text_value",
    "preview",
    "project",
    "source",
    "story",
    "tag",
    "target",
    "task",
    "sticker_name",
    "custom_field",
    "type",
)

# List of team fields to be retrieved from Asana API
TEAMS_FIELD = (
    "gid",
    "resource_type",
    "name",
    "description",
    "organization",
    "permalink_url",
    "visibility",
)

# List of user fields to be retrieved from Asana API
USER_FIELDS = ("gid", "resource_type", "name", "email", "photo", "workspaces")
