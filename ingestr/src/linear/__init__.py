from typing import Any, Dict, Iterable, Iterator

import dlt
import pendulum

from .helpers import (
    _create_paginated_resource,
    _get_date_range,
    _graphql,
    normalize_dictionaries,
)

ISSUES_QUERY = """
query Issues($cursor: String) {
  issues(first: 50, after: $cursor) {
    nodes {
      id
      title
      description
      createdAt
      updatedAt
      archivedAt
      addedToCycleAt
      autoArchivedAt
      autoClosedAt
      boardOrder
      branchName
      canceledAt
      completedAt
      customerTicketCount
      descriptionState
      dueDate
      estimate
      identifier
      integrationSourceType
      labelIds
      number
      previousIdentifiers
      priority
      priorityLabel
      prioritySortOrder
      reactionData
      slaBreachesAt
      slaHighRiskAt
      slaMediumRiskAt
      slaStartedAt
      slaType
      snoozedUntilAt
      sortOrder
      startedAt
      startedTriageAt
      subIssueSortOrder
      triagedAt
      url
      
      creator { id }
      assignee { id }
      botActor { id name type }
      cycle { id }
      delegate { id }
      externalUserCreator { id }
      favorite { id }
      lastAppliedTemplate { id }
      parent { id }
      projectMilestone { id }
      recurringIssueTemplate { id }
      snoozedBy { id }
      sourceComment { id }
      state { id }
      
      labels(first: 250) { 
        nodes { 
          id 
        } 
      }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""

ATTACHMENTS_QUERY = """
query Attachments($cursor: String) {
  attachments(first: 50, after: $cursor) {
    nodes {
      id
      archivedAt
      bodyData
      createdAt
      groupBySource
      metadata
      sourceType
      subtitle
      title
      updatedAt
      url
      
      creator { id }
      externalUserCreator { id }
      issue { id }
      originalIssue { id }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""

COMMENTS_QUERY = """
query Comments($cursor: String) {
  comments(first: 50, after: $cursor) {
    nodes {
      id
      archivedAt
      body
      bodyData
      createdAt
      editedAt
      quotedText
      reactionData 
      resolvedAt
      threadSummary
      updatedAt
      url
      
      botActor { id  }
      documentContent { id }
      externalThread { id }
      externalUser { id }
      initiativeUpdate { id }
      issue { id }
      parent { id }
      post { id }
      projectUpdate { id }
      resolvingComment { id }
      resolvingUser { id }
      user { id }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""

CYCLES_QUERY = """
query Cycles($cursor: String) {
  cycles(first: 50, after: $cursor) {
    nodes {
      id
      archivedAt
      autoArchivedAt
      completedAt
      completedIssueCountHistory
      completedScopeHistory
      createdAt
      description
      endsAt
      inProgressScopeHistory
      issueCountHistory
      name
      number
      progress
      scopeHistory
      startsAt
      updatedAt
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""

DOCUMENTS_QUERY = """
query Documents($cursor: String) {
  documents(first: 50, after: $cursor) {
    nodes {
      id
      archivedAt
      color
      createdAt
      icon
      slugId
      title
      updatedAt
      
      creator { id }
      updatedBy { id }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""

EXTERNAL_USERS_QUERY = """
query ExternalUsers($cursor: String) {
  externalUsers(first: 50, after: $cursor) {
    nodes {
      id
      archivedAt
      avatarUrl
      createdAt
      displayName
      email
      lastSeen
      name
      updatedAt
      
      organization { id }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""

INITIATIVES_QUERY = """
query Initiatives($cursor: String) {
  initiatives(first: 50, after: $cursor) {
    nodes {
      id
      archivedAt
      color
      completedAt
      content
      createdAt
      description
      frequencyResolution
      health
      healthUpdatedAt
      icon
      name
      slugId
      sortOrder
      startedAt
      status
      targetDate
      targetDateResolution
      trashed
      updateReminderFrequency
      updateReminderFrequencyInWeeks
      updateRemindersDay
      updateRemindersHour
      updatedAt
      
      creator { id }
      documentContent { id }
      integrationsSettings { id }
      lastUpdate { id }
      organization { id }
      owner { id }
      parentInitiative { id }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""


INITIATIVE_TO_PROJECTS_QUERY = """
query InitiativeToProjects($cursor: String) {
  initiativeToProjects(first: 50, after: $cursor) {
    nodes {
      id
      archivedAt
      createdAt
      sortOrder
      updatedAt
      
      initiative { id }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""

PROJECT_MILESTONES_QUERY = """
query ProjectMilestones($cursor: String) {
  projectMilestones(first: 50, after: $cursor) {
    nodes {
      id
      archivedAt
      createdAt
      currentProgress
      description
      descriptionState
      name
      progress
      progressHistory
      sortOrder
      status
      targetDate
      updatedAt
      
      documentContent { id }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""

PROJECT_STATUSES_QUERY = """
query ProjectStatuses($cursor: String) {
  projectStatuses(first: 50, after: $cursor) {
    nodes {
      id
      archivedAt
      color
      createdAt
      description
      indefinite
      name
      position
      type
      updatedAt
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""

INTEGRATIONS_QUERY = """
query Integrations($cursor: String) {
  integrations(first: 50, after: $cursor) {
    nodes {
      id
      archivedAt
      createdAt
      service
      updatedAt
      
      creator { id }
      organization { id }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""


LABELS_QUERY = """
query IssueLabels($cursor: String) {
  issueLabels(first: 50, after: $cursor) {
    nodes {
      id
      archivedAt
      color
      createdAt
      description
      name
      updatedAt
      
      creator { id }
      organization { id }
      parent { id }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""


ORGANIZATION_QUERY = """
query Organization {
  viewer {
    organization {
      id
      name
      createdAt
      updatedAt
      archivedAt
      logoUrl
      allowMembersToInvite
      allowedAuthServices
      createdIssueCount
      customerCount
      customersEnabled
      deletionRequestedAt
      gitBranchFormat
      gitLinkbackMessagesEnabled
      gitPublicLinkbackMessagesEnabled
      logoUrl
      periodUploadVolume
      previousUrlKeys
      roadmapEnabled
      samlEnabled
      scimEnabled
    }
  }
}
"""


PROJECT_UPDATES_QUERY = """
query ProjectUpdates($cursor: String) {
  projectUpdates(first: 50, after: $cursor) {
    nodes {
      id
      archivedAt
      body
      bodyData
      createdAt
      diffMarkdown
      health
      updatedAt
      url
      
      user { id }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""


TEAM_MEMBERSHIPS_QUERY = """
query TeamMemberships($cursor: String) {
  teamMemberships(first: 50, after: $cursor) {
    nodes {
      id
      archivedAt
      createdAt
      owner
      sortOrder
      updatedAt
      
      user { id }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""

USERS_QUERY = """
query Users($cursor: String) {
  users(first: 50, after: $cursor) {
    nodes {
      id
      active
      admin
      archivedAt
      avatarUrl
      calendarHash
      createdAt
      createdIssueCount
      description
      disableReason
      displayName
      email
      guest
      inviteHash
      lastSeen
      name
      statusEmoji
      statusLabel
      statusUntilAt
      timezone
      updatedAt
      url
      
      organization { id }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""
WORKFLOW_STATES_QUERY = """
query WorkflowStates($cursor: String) {
  workflowStates(first: 50, after: $cursor) {
    nodes {
      id
      archivedAt
      color
      createdAt
      description
      name
      position
      type
      updatedAt
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""
PROJECTS_QUERY = """
query Projects($cursor: String) {
  projects(first: 50, after: $cursor) {
    nodes {
      id
      name
      description
      createdAt
      updatedAt
      archivedAt
      completedAt
      canceledAt
      startedAt
      
      color
      icon
      slugId
      url
      
      health
      priority
      priorityLabel
      state
      
      targetDate
      startDate
      
      progress
      currentProgress
      scope
      
      sortOrder
      trashed
      
      creator { id }
      lead { id }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""

TEAMS_QUERY = """
query Teams($cursor: String) {
  teams(first: 50, after: $cursor) {
    nodes {
      id
      name
      key
      description
      color
      icon
      private
      archivedAt
      createdAt
      updatedAt
      
      organization { id }
      parent { id }
      
      cyclesEnabled
      cycleDuration
      cycleStartDay
      cycleCooldownTime
      
      issueCount
      issueEstimationType
      issueEstimationAllowZero
      issueEstimationExtended
      issueOrderingNoPriorityFirst
      
      autoArchivePeriod
      autoClosePeriod
      autoCloseChildIssues
      autoCloseParentIssues
      
      groupIssueHistory
      timezone
      inviteHash
      joinByDefault
      
      slackNewIssue
      slackIssueComments
      slackIssueStatuses
      
      triageEnabled
      requirePriorityToLeaveTriage
      upcomingCycleCount
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""


# Paginated resources configuration
PAGINATED_RESOURCES = [
    ("issues", ISSUES_QUERY, "issues"),
    ("users", USERS_QUERY, "users"),
    ("workflow_states", WORKFLOW_STATES_QUERY, "workflowStates"),
    ("cycles", CYCLES_QUERY, "cycles"),
    ("attachments", ATTACHMENTS_QUERY, "attachments"),
    ("comments", COMMENTS_QUERY, "comments"),
    ("documents", DOCUMENTS_QUERY, "documents"),
    ("external_users", EXTERNAL_USERS_QUERY, "externalUsers"),
    ("initiative", INITIATIVES_QUERY, "initiatives"),
    ("integrations", INTEGRATIONS_QUERY, "integrations"),
    ("labels", LABELS_QUERY, "issueLabels"),
    ("project_updates", PROJECT_UPDATES_QUERY, "projectUpdates"),
    ("team_memberships", TEAM_MEMBERSHIPS_QUERY, "teamMemberships"),
    ("initiative_to_project", INITIATIVE_TO_PROJECTS_QUERY, "initiativeToProjects"),
    ("project_milestone", PROJECT_MILESTONES_QUERY, "projectMilestones"),
    ("project_status", PROJECT_STATUSES_QUERY, "projectStatuses"),
    ("projects", PROJECTS_QUERY, "projects"),
    ("teams", TEAMS_QUERY, "teams"),
]


@dlt.source(name="linear", max_table_nesting=0)
def linear_source(
    api_key: str,
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime | None = None,
) -> Iterable[dlt.sources.DltResource]:
    @dlt.resource(name="organization", primary_key="id", write_disposition="merge")
    def organization(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        data = _graphql(api_key, ORGANIZATION_QUERY)
        if "viewer" in data and "organization" in data["viewer"]:
            item = data["viewer"]["organization"]
            if item and pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    # Create paginated resources dynamically
    paginated_resources = [
        _create_paginated_resource(
            resource_name, query, query_field, api_key, start_date, end_date
        )
        for resource_name, query, query_field in PAGINATED_RESOURCES
    ]

    return [
        *paginated_resources,
        organization,
    ]
