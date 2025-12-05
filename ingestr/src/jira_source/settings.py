# Copyright 2022-2025 ScaleVector
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Jira source settings and constants"""

# Default start date for Jira API requests
DEFAULT_START_DATE = "2010-01-01"

# Jira API request timeout in seconds
REQUEST_TIMEOUT = 300

# Default page size for paginated requests
DEFAULT_PAGE_SIZE = 100

# Maximum page size allowed by Jira API
MAX_PAGE_SIZE = 1000

# Base API path for Jira Cloud
API_BASE_PATH = "/rest/api/3"

# Project fields to retrieve from Jira API
PROJECT_FIELDS = (
    "id",
    "key",
    "name",
    "description",
    "lead",
    "projectCategory",
    "projectTypeKey",
    "simplified",
    "style",
    "favourite",
    "isPrivate",
    "properties",
    "entityId",
    "uuid",
    "insight",
)

# Issue fields to retrieve from Jira API
ISSUE_FIELDS = (
    "id",
    "key",
    "summary",
    "description",
    "issuetype",
    "status",
    "priority",
    "resolution",
    "assignee",
    "reporter",
    "creator",
    "created",
    "updated",
    "resolutiondate",
    "duedate",
    "components",
    "fixVersions",
    "versions",
    "labels",
    "environment",
    "project",
    "parent",
    "subtasks",
    "issuelinks",
    "votes",
    "watches",
    "worklog",
    "attachments",
    "comment",
    "customfield_*",
)

# User fields to retrieve from Jira API
USER_FIELDS = (
    "accountId",
    "accountType",
    "emailAddress",
    "displayName",
    "active",
    "timeZone",
    "groups",
    "applicationRoles",
    "expand",
)

# Board fields to retrieve from Jira API (for Agile/Scrum boards)
BOARD_FIELDS = (
    "id",
    "name",
    "type",
    "location",
    "filter",
    "subQuery",
)

# Sprint fields to retrieve from Jira API
SPRINT_FIELDS = (
    "id",
    "name",
    "state",
    "startDate",
    "endDate",
    "completeDate",
    "originBoardId",
    "goal",
)

# Issue type fields to retrieve from Jira API
ISSUE_TYPE_FIELDS = (
    "id",
    "name",
    "description",
    "iconUrl",
    "subtask",
    "avatarId",
    "hierarchyLevel",
)

# Status fields to retrieve from Jira API
STATUS_FIELDS = (
    "id",
    "name",
    "description",
    "iconUrl",
    "statusCategory",
)

# Priority fields to retrieve from Jira API
PRIORITY_FIELDS = (
    "id",
    "name",
    "description",
    "iconUrl",
)

# Resolution fields to retrieve from Jira API
RESOLUTION_FIELDS = (
    "id",
    "name",
    "description",
)

# Version fields to retrieve from Jira API
VERSION_FIELDS = (
    "id",
    "name",
    "description",
    "archived",
    "released",
    "startDate",
    "releaseDate",
    "overdue",
    "userStartDate",
    "userReleaseDate",
    "project",
    "projectId",
)

# Component fields to retrieve from Jira API
COMPONENT_FIELDS = (
    "id",
    "name",
    "description",
    "lead",
    "assigneeType",
    "assignee",
    "realAssigneeType",
    "realAssignee",
    "isAssigneeTypeValid",
    "project",
    "projectId",
)
