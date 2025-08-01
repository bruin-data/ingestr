from typing import Any, Dict, Iterable, Iterator

import dlt
import pendulum
import requests

from .helpers import _paginate, _graphql, normalize_dictionaries


def _get_date_range(updated_at, start_date):
    """Extract current start and end dates from incremental state."""
    if updated_at.last_value:
        current_start_date = pendulum.parse(updated_at.last_value)
    else:
        current_start_date = pendulum.parse(start_date)

    if updated_at.end_value:
        current_end_date = pendulum.parse(updated_at.end_value)
    else:
        current_end_date = pendulum.now(tz="UTC")

    return current_start_date, current_end_date


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
      addedToProjectAt
      addedToTeamAt
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
      project { id }
      projectMilestone { id }
      recurringIssueTemplate { id }
      snoozedBy { id }
      sourceComment { id }
      state { id }
      team { id }
      
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
      
      botActor { id name type }
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
      
      team { id }
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
      project { id }
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
      project { id }
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
      project { id }
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
      team { id }
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
      team { id }
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

PROJECTS_QUERY = """
query Projects($cursor: String) {
  projects(first: 50, after: $cursor) {
    nodes {
      id
      archivedAt
      autoArchivedAt
      canceledAt
      color
      completedAt
      completedIssueCountHistory
      completedScopeHistory
      content
      createdAt
      description
      health
      icon
      inProgressScopeHistory
      issueCountHistory
      name
      priority
      progress
      scopeHistory
      slugId
      sortOrder
      startDate
      startedAt
      state
      targetDate
      updatedAt
      
      creator { id }
      favorite { id }
      lead { id }
      organization { id }
      team { id }
    }
    pageInfo { hasNextPage endCursor }
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
      
      project { id }
      user { id }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""

ROADMAPS_QUERY = """
query Roadmaps($cursor: String) {
  roadmaps(first: 50, after: $cursor) {
    nodes {
      id
      archivedAt
      createdAt
      description
      name
      slugId
      updatedAt
      
      creator { id }
      organization { id }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""

ROADMAP_TO_PROJECTS_QUERY = """
query RoadmapToProjects($cursor: String) {
  roadmapToProjects(first: 50, after: $cursor) {
    nodes {
      id
      archivedAt
      createdAt
      sortOrder
      updatedAt
      
      project { id }
      roadmap { id }
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
      archivedAt
      autoArchivePeriod
      autoClosePeriod
      autoCloseStateId
      color
      createdAt
      cycleCalenderUrl
      cycleCurrentIssueNumber
      cycleDuration
      cycleEnabledStartWeek
      cycleIssueAutoAssignCompleted
      cycleIssueAutoAssignStarted
      cycleLockToActive
      cycleStartDay
      cyclesEnabled
      defaultIssueEstimate
      defaultTemplateForMembersId
      defaultTemplateForNonMembersId
      description
      draftWorkflowStateId
      estimationAllowZero
      estimationExtended
      estimationType
      groupIssueHistory
      icon
      issueEstimationAllowZero
      issueEstimationExtended
      issueEstimationType
      issueOrderingNeedsUsernameInDisplayName
      issueSortOrderDefaultToBottom
      key
      markedAsDuplicateWorkflowStateId
      mergeWorkflowStateId
      name
      private
      requirePriorityToLeaveTriage
      scimGroupName
      slackIssueComments
      slackIssueStatuses
      slackNewIssue
      timezone
      triageEnabled
      upcomingCycleCount
      updatedAt
      
      activeCycle { id }
      defaultIssueState { id }
      defaultProjectTemplate { id }
      defaultTemplate { id }
      draftWorkflowState { id }
      integrationsSettings { id }
      markedAsDuplicateWorkflowState { id }
      mergeWorkflowState { id }
      organization { id }
      parent { id }
      reviewWorkflowState { id }
      startWorkflowState { id }
      triageIssueState { id }
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
      
      team { id }
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
      
      team { id }
    }
    pageInfo { hasNextPage endCursor }
  }
}
"""


@dlt.source(name="linear", max_table_nesting=0)
def linear_source(
    api_key: str,
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime | None = None,
) -> Iterable[dlt.sources.DltResource]:
    @dlt.resource(name="issues", primary_key="id", write_disposition="merge")
    def issues(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, ISSUES_QUERY, "issues"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    @dlt.resource(name="projects", primary_key="id", write_disposition="merge")
    def projects(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, PROJECTS_QUERY, "projects"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    @dlt.resource(name="teams", primary_key="id", write_disposition="merge")
    def teams(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, TEAMS_QUERY, "teams"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    @dlt.resource(name="users", primary_key="id", write_disposition="merge")
    def users(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, USERS_QUERY, "users"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    @dlt.resource(name="workflow_states", primary_key="id", write_disposition="merge")
    def workflow_states(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, WORKFLOW_STATES_QUERY, "workflowStates"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    @dlt.resource(name="cycles", primary_key="id", write_disposition="merge")
    def cycles(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, CYCLES_QUERY, "cycles"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    @dlt.resource(name="attachments", primary_key="id", write_disposition="merge")
    def attachments(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, ATTACHMENTS_QUERY, "attachments"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    @dlt.resource(name="comments", primary_key="id", write_disposition="merge")
    def comments(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, COMMENTS_QUERY, "comments"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    @dlt.resource(name="documents", primary_key="id", write_disposition="merge")
    def documents(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, DOCUMENTS_QUERY, "documents"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    @dlt.resource(name="external_users", primary_key="id", write_disposition="merge")
    def external_users(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, EXTERNAL_USERS_QUERY, "externalUsers"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    @dlt.resource(name="initiative", primary_key="id", write_disposition="merge")
    def initiative(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, INITIATIVES_QUERY, "initiatives"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    @dlt.resource(name="integrations", primary_key="id", write_disposition="merge")
    def integrations(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, INTEGRATIONS_QUERY, "integrations"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)


    @dlt.resource(name="labels", primary_key="id", write_disposition="merge")
    def labels(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, LABELS_QUERY, "issueLabels"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

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

    @dlt.resource(name="project_updates", primary_key="id", write_disposition="merge")
    def project_updates(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, PROJECT_UPDATES_QUERY, "projectUpdates"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    @dlt.resource(name="roadmaps", primary_key="id", write_disposition="merge")
    def roadmaps(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, ROADMAPS_QUERY, "roadmaps"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    @dlt.resource(name="roadmap_to_projects", primary_key="id", write_disposition="merge")
    def roadmap_to_projects(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, ROADMAP_TO_PROJECTS_QUERY, "roadmapToProjects"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    @dlt.resource(name="team_memberships", primary_key="id", write_disposition="merge")
    def team_memberships(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, TEAM_MEMBERSHIPS_QUERY, "teamMemberships"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)



    @dlt.resource(name="initiative_to_project", primary_key="id", write_disposition="merge")
    def initiative_to_project(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, INITIATIVE_TO_PROJECTS_QUERY, "initiativeToProjects"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    @dlt.resource(name="project_milestone", primary_key="id", write_disposition="merge")
    def project_milestone(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, PROJECT_MILESTONES_QUERY, "projectMilestones"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    @dlt.resource(name="project_status", primary_key="id", write_disposition="merge")
    def project_status(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        current_start_date, current_end_date = _get_date_range(updated_at, start_date)

        for item in _paginate(api_key, PROJECT_STATUSES_QUERY, "projectStatuses"):
            if pendulum.parse(item["updatedAt"]) >= current_start_date:
                if pendulum.parse(item["updatedAt"]) <= current_end_date:
                    yield normalize_dictionaries(item)

    return [
        issues, 
        projects, 
        teams, 
        users, 
        workflow_states, 
        cycles,
        attachments,
        comments,
        documents,
        external_users,
        initiative,
        integrations,
        labels,
        organization,
        project_updates,
        roadmaps,
        roadmap_to_projects,
        team_memberships,
        initiative_to_project,
        project_milestone,
        project_status,
    ]

