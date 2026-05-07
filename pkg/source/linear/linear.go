package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	ingestrhttp "github.com/bruin-data/gong/pkg/http"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

const (
	graphQLBaseURL = "https://api.linear.app/graphql"
	maxPageSize    = 50 // Linear API limit is 50
	rateLimit      = 1  // Linear allows only 5,000 req/hour (~83/min), much stricter than similar APIs
	rateLimitBurst = 3
)

var supportedTables = []string{
	"issues",
	"attachments",
	"comments",
	"cycles",
	"documents",
	"external_users",
	"initiatives",
	"initiative_to_projects",
	"project_milestones",
	"project_statuses",
	"integrations",
	"issue_labels",
	"organization",
	"project_updates",
	"team_memberships",
	"users",
	"workflow_states",
	"projects",
	"teams",
}

var tablesWithFilterSupport = map[string]bool{
	"issues":         true,
	"attachments":    true,
	"comments":       true,
	"cycles":         true,
	"documents":      true,
	"initiatives":    true,
	"issueLabels":    true,
	"projectUpdates": true,
	// These tables DO NOT support filter parameter:
	// "externalUsers": false,
	// "initiativeToProjects": false,
	// "projectMilestones": false,
	// "projectStatuses": false,
	// "integrations": false,
	// "teamMemberships": false,
	// "users": false,
	// "workflowStates": false,
	// "projects": false,
	// "teams": false,
}

var issueFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "title", DataType: schema.TypeString, Nullable: true},
	{Name: "description", DataType: schema.TypeString, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "addedToCycleAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "autoArchivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "autoClosedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "boardOrder", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "branchName", DataType: schema.TypeString, Nullable: true},
	{Name: "canceledAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "completedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "customerTicketCount", DataType: schema.TypeInt64, Nullable: true},
	{Name: "descriptionState", DataType: schema.TypeString, Nullable: true},
	{Name: "dueDate", DataType: schema.TypeString, Nullable: true},
	{Name: "estimate", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "identifier", DataType: schema.TypeString, Nullable: true},
	{Name: "integrationSourceType", DataType: schema.TypeString, Nullable: true},
	{Name: "labelIds", DataType: schema.TypeJSON, Nullable: true},
	{Name: "number", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "previousIdentifiers", DataType: schema.TypeJSON, Nullable: true},
	{Name: "priority", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "priorityLabel", DataType: schema.TypeString, Nullable: true},
	{Name: "prioritySortOrder", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "reactionData", DataType: schema.TypeJSON, Nullable: true},
	{Name: "slaBreachesAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "slaHighRiskAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "slaMediumRiskAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "slaStartedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "slaType", DataType: schema.TypeString, Nullable: true},
	{Name: "snoozedUntilAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "sortOrder", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "startedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "startedTriageAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "subIssueSortOrder", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "triagedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "url", DataType: schema.TypeString, Nullable: true},
	{Name: "creatorId", DataType: schema.TypeString, Nullable: true},
	{Name: "assigneeId", DataType: schema.TypeString, Nullable: true},
	{Name: "botActor", DataType: schema.TypeJSON, Nullable: true}, // Keep as JSON - has multiple fields (id, name, type)
	{Name: "cycleId", DataType: schema.TypeString, Nullable: true},
	{Name: "delegateId", DataType: schema.TypeString, Nullable: true},
	{Name: "externalUserCreatorId", DataType: schema.TypeString, Nullable: true},
	{Name: "favoriteId", DataType: schema.TypeString, Nullable: true},
	{Name: "lastAppliedTemplateId", DataType: schema.TypeString, Nullable: true},
	{Name: "parentId", DataType: schema.TypeString, Nullable: true},
	{Name: "projectMilestoneId", DataType: schema.TypeString, Nullable: true},
	{Name: "recurringIssueTemplateId", DataType: schema.TypeString, Nullable: true},
	{Name: "snoozedById", DataType: schema.TypeString, Nullable: true},
	{Name: "sourceCommentId", DataType: schema.TypeString, Nullable: true},
	{Name: "stateId", DataType: schema.TypeString, Nullable: true},
	{Name: "labels", DataType: schema.TypeJSON, Nullable: true},
}

var attachmentFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "bodyData", DataType: schema.TypeString, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "groupBySource", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "metadata", DataType: schema.TypeJSON, Nullable: true},
	{Name: "sourceType", DataType: schema.TypeString, Nullable: true},
	{Name: "subtitle", DataType: schema.TypeString, Nullable: true},
	{Name: "title", DataType: schema.TypeString, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "url", DataType: schema.TypeString, Nullable: true},
	{Name: "creatorId", DataType: schema.TypeString, Nullable: true},
	{Name: "externalUserCreatorId", DataType: schema.TypeString, Nullable: true},
	{Name: "issueId", DataType: schema.TypeString, Nullable: true},
	{Name: "originalIssueId", DataType: schema.TypeString, Nullable: true},
}

var commentFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "body", DataType: schema.TypeString, Nullable: true},
	{Name: "bodyData", DataType: schema.TypeString, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "editedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "quotedText", DataType: schema.TypeString, Nullable: true},
	{Name: "reactionData", DataType: schema.TypeJSON, Nullable: true},
	{Name: "resolvedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "threadSummary", DataType: schema.TypeJSON, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "url", DataType: schema.TypeString, Nullable: true},
	{Name: "botActorId", DataType: schema.TypeString, Nullable: true},
	{Name: "documentContentId", DataType: schema.TypeString, Nullable: true},
	{Name: "externalThreadId", DataType: schema.TypeString, Nullable: true},
	{Name: "externalUserId", DataType: schema.TypeString, Nullable: true},
	{Name: "initiativeUpdateId", DataType: schema.TypeString, Nullable: true},
	{Name: "issueId", DataType: schema.TypeString, Nullable: true},
	{Name: "parentId", DataType: schema.TypeString, Nullable: true},
	{Name: "postId", DataType: schema.TypeString, Nullable: true},
	{Name: "projectUpdateId", DataType: schema.TypeString, Nullable: true},
	{Name: "resolvingCommentId", DataType: schema.TypeString, Nullable: true},
	{Name: "resolvingUserId", DataType: schema.TypeString, Nullable: true},
	{Name: "userId", DataType: schema.TypeString, Nullable: true},
}

var cycleFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "autoArchivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "completedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "completedIssueCountHistory", DataType: schema.TypeJSON, Nullable: true},
	{Name: "completedScopeHistory", DataType: schema.TypeJSON, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "endsAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "description", DataType: schema.TypeString, Nullable: true},
	{Name: "inProgressScopeHistory", DataType: schema.TypeJSON, Nullable: true},
	{Name: "issueCountHistory", DataType: schema.TypeJSON, Nullable: true},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "number", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "progress", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "scopeHistory", DataType: schema.TypeJSON, Nullable: true},
	{Name: "startsAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
}

var documentFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "color", DataType: schema.TypeString, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "icon", DataType: schema.TypeString, Nullable: true},
	{Name: "slugId", DataType: schema.TypeString, Nullable: true},
	{Name: "title", DataType: schema.TypeString, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "creatorId", DataType: schema.TypeString, Nullable: true},
	{Name: "updatedById", DataType: schema.TypeString, Nullable: true},
}

var externalUserFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "avatarUrl", DataType: schema.TypeString, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "displayName", DataType: schema.TypeString, Nullable: true},
	{Name: "email", DataType: schema.TypeString, Nullable: true},
	{Name: "lastSeen", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "organizationId", DataType: schema.TypeString, Nullable: true},
}

var initiativeFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "color", DataType: schema.TypeString, Nullable: true},
	{Name: "completedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "content", DataType: schema.TypeString, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "description", DataType: schema.TypeString, Nullable: true},
	{Name: "frequencyResolution", DataType: schema.TypeString, Nullable: true},
	{Name: "health", DataType: schema.TypeString, Nullable: true},
	{Name: "healthUpdatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "icon", DataType: schema.TypeString, Nullable: true},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "slugId", DataType: schema.TypeString, Nullable: true},
	{Name: "sortOrder", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "startedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "status", DataType: schema.TypeString, Nullable: true},
	{Name: "targetDate", DataType: schema.TypeString, Nullable: true},
	{Name: "targetDateResolution", DataType: schema.TypeString, Nullable: true},
	{Name: "trashed", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "updateReminderFrequency", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "updateReminderFrequencyInWeeks", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "updateRemindersDay", DataType: schema.TypeString, Nullable: true},
	{Name: "updateRemindersHour", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "creatorId", DataType: schema.TypeString, Nullable: true},
	{Name: "documentContentId", DataType: schema.TypeString, Nullable: true},
	{Name: "integrationsSettingsId", DataType: schema.TypeString, Nullable: true},
	{Name: "lastUpdateId", DataType: schema.TypeString, Nullable: true},
	{Name: "organizationId", DataType: schema.TypeString, Nullable: true},
	{Name: "ownerId", DataType: schema.TypeString, Nullable: true},
	{Name: "parentInitiativeId", DataType: schema.TypeString, Nullable: true},
}

var initiativeToProjectFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "sortOrder", DataType: schema.TypeString, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "initiativeId", DataType: schema.TypeString, Nullable: true},
}

var projectMilestoneFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "currentProgress", DataType: schema.TypeJSON, Nullable: true},
	{Name: "description", DataType: schema.TypeString, Nullable: true},
	{Name: "descriptionState", DataType: schema.TypeString, Nullable: true},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "progress", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "progressHistory", DataType: schema.TypeJSON, Nullable: true},
	{Name: "sortOrder", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "status", DataType: schema.TypeString, Nullable: true},
	{Name: "targetDate", DataType: schema.TypeString, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "documentContentId", DataType: schema.TypeString, Nullable: true},
}

var projectStatusFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "color", DataType: schema.TypeString, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "description", DataType: schema.TypeString, Nullable: true},
	{Name: "indefinite", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "position", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "type", DataType: schema.TypeString, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
}

var integrationFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "service", DataType: schema.TypeString, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "creatorId", DataType: schema.TypeString, Nullable: true},
	{Name: "organizationId", DataType: schema.TypeString, Nullable: true},
}

var issueLabelFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "color", DataType: schema.TypeString, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "description", DataType: schema.TypeString, Nullable: true},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "creatorId", DataType: schema.TypeString, Nullable: true},
	{Name: "organizationId", DataType: schema.TypeString, Nullable: true},
	{Name: "parentId", DataType: schema.TypeString, Nullable: true},
}

var organizationFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "logoUrl", DataType: schema.TypeString, Nullable: true},
	{Name: "allowMembersToInvite", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "allowedAuthServices", DataType: schema.TypeJSON, Nullable: true},
	{Name: "createdIssueCount", DataType: schema.TypeInt64, Nullable: true},
	{Name: "customerCount", DataType: schema.TypeInt64, Nullable: true},
	{Name: "customersEnabled", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "deletionRequestedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "gitBranchFormat", DataType: schema.TypeString, Nullable: true},
	{Name: "gitLinkbackMessagesEnabled", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "gitPublicLinkbackMessagesEnabled", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "periodUploadVolume", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "previousUrlKeys", DataType: schema.TypeJSON, Nullable: true},
	{Name: "roadmapEnabled", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "samlEnabled", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "scimEnabled", DataType: schema.TypeBoolean, Nullable: true},
}

var projectUpdateFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "body", DataType: schema.TypeString, Nullable: true},
	{Name: "bodyData", DataType: schema.TypeString, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "diffMarkdown", DataType: schema.TypeString, Nullable: true},
	{Name: "health", DataType: schema.TypeString, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "url", DataType: schema.TypeString, Nullable: true},
	{Name: "userId", DataType: schema.TypeString, Nullable: true},
}

var teamMembershipFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "owner", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "sortOrder", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "userId", DataType: schema.TypeString, Nullable: true},
}

var userFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "active", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "admin", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "avatarUrl", DataType: schema.TypeString, Nullable: true},
	{Name: "calendarHash", DataType: schema.TypeString, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "createdIssueCount", DataType: schema.TypeInt64, Nullable: true},
	{Name: "description", DataType: schema.TypeString, Nullable: true},
	{Name: "disableReason", DataType: schema.TypeString, Nullable: true},
	{Name: "displayName", DataType: schema.TypeString, Nullable: true},
	{Name: "email", DataType: schema.TypeString, Nullable: true},
	{Name: "guest", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "inviteHash", DataType: schema.TypeString, Nullable: true},
	{Name: "lastSeen", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "statusEmoji", DataType: schema.TypeString, Nullable: true},
	{Name: "statusLabel", DataType: schema.TypeString, Nullable: true},
	{Name: "statusUntilAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "timezone", DataType: schema.TypeString, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "url", DataType: schema.TypeString, Nullable: true},
	{Name: "organizationId", DataType: schema.TypeString, Nullable: true},
}

var workflowStateFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "color", DataType: schema.TypeString, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "description", DataType: schema.TypeString, Nullable: true},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "position", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "type", DataType: schema.TypeString, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
}

var projectFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "description", DataType: schema.TypeString, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "completedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "canceledAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "startedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "color", DataType: schema.TypeString, Nullable: true},
	{Name: "icon", DataType: schema.TypeString, Nullable: true},
	{Name: "slugId", DataType: schema.TypeString, Nullable: true},
	{Name: "url", DataType: schema.TypeString, Nullable: true},
	{Name: "health", DataType: schema.TypeString, Nullable: true},
	{Name: "priority", DataType: schema.TypeInt64, Nullable: true},
	{Name: "priorityLabel", DataType: schema.TypeString, Nullable: true},
	{Name: "state", DataType: schema.TypeString, Nullable: true},
	{Name: "targetDate", DataType: schema.TypeString, Nullable: true},
	{Name: "startDate", DataType: schema.TypeString, Nullable: true},
	{Name: "progress", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "currentProgress", DataType: schema.TypeJSON, Nullable: true},
	{Name: "scope", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "sortOrder", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "trashed", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "creatorId", DataType: schema.TypeString, Nullable: true},
	{Name: "leadId", DataType: schema.TypeString, Nullable: true},
}

var teamFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "key", DataType: schema.TypeString, Nullable: true},
	{Name: "description", DataType: schema.TypeString, Nullable: true},
	{Name: "color", DataType: schema.TypeString, Nullable: true},
	{Name: "icon", DataType: schema.TypeString, Nullable: true},
	{Name: "private", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "archivedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "organizationId", DataType: schema.TypeString, Nullable: true},
	{Name: "parentId", DataType: schema.TypeString, Nullable: true},
	{Name: "cyclesEnabled", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "cycleDuration", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "cycleStartDay", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "cycleCooldownTime", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "issueCount", DataType: schema.TypeInt64, Nullable: true},
	{Name: "issueEstimationType", DataType: schema.TypeString, Nullable: true},
	{Name: "issueEstimationAllowZero", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "issueEstimationExtended", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "issueOrderingNoPriorityFirst", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "autoArchivePeriod", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "autoClosePeriod", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "autoCloseChildIssues", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "autoCloseParentIssues", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "groupIssueHistory", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "timezone", DataType: schema.TypeString, Nullable: true},
	{Name: "inviteHash", DataType: schema.TypeString, Nullable: true},
	{Name: "joinByDefault", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "slackNewIssue", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "slackIssueComments", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "slackIssueStatuses", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "triageEnabled", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "requirePriorityToLeaveTriage", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "upcomingCycleCount", DataType: schema.TypeFloat64, Nullable: true},
}

type LinearSource struct {
	apiKey string
	client *ingestrhttp.Client
}

func NewLinearSource() *LinearSource {
	return &LinearSource{}
}

func (s *LinearSource) HandlesIncrementality() bool {
	return true
}

func (s *LinearSource) Schemes() []string {
	return []string{"linear"}
}

func (s *LinearSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseAPIKeyFromURI(uri)
	if err != nil {
		return err
	}
	s.apiKey = apiKey

	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(graphQLBaseURL),
		ingestrhttp.WithTimeout(60*time.Second),
		ingestrhttp.WithRateLimiter(rateLimit, rateLimitBurst),
		ingestrhttp.WithDebug(config.DebugMode),
		ingestrhttp.WithHeader("Authorization", s.apiKey),
		ingestrhttp.WithHeader("Content-Type", "application/json"),
	)
	config.Debug("[LINEAR] Connected successfully")
	return nil
}

func parseAPIKeyFromURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "linear://") {
		return "", fmt.Errorf("invalid linear URI: must start with linear://")
	}

	rest := strings.TrimPrefix(uri, "linear://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in URI query parameters")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse linear URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key query parameter is required")
	}

	return apiKey, nil
}

func (s *LinearSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *LinearSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name
	tableSchema, err := s.getSchema(ctx, tableName)
	if err != nil {
		return nil, err
	}

	incrementalKey := "updatedAt"

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    tableSchema.PrimaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func (s *LinearSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	var columns []schema.Column

	switch table {
	case "issues":
		columns = issueFields
	case "attachments":
		columns = attachmentFields
	case "comments":
		columns = commentFields
	case "cycles":
		columns = cycleFields
	case "documents":
		columns = documentFields
	case "external_users":
		columns = externalUserFields
	case "initiatives":
		columns = initiativeFields
	case "initiative_to_projects":
		columns = initiativeToProjectFields
	case "project_milestones":
		columns = projectMilestoneFields
	case "project_statuses":
		columns = projectStatusFields
	case "integrations":
		columns = integrationFields
	case "issue_labels":
		columns = issueLabelFields
	case "organization":
		columns = organizationFields
	case "project_updates":
		columns = projectUpdateFields
	case "team_memberships":
		columns = teamMembershipFields
	case "users":
		columns = userFields
	case "workflow_states":
		columns = workflowStateFields
	case "projects":
		columns = projectFields
	case "teams":
		columns = teamFields
	default:
		return nil, fmt.Errorf("unsupported table: %s", table)
	}

	return &schema.TableSchema{
		Name:        table,
		Columns:     columns,
		PrimaryKeys: []string{"id"},
	}, nil
}

func (s *LinearSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if !isValidTable(table) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", table, strings.Join(supportedTables, ", "))
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "issues":
			err = s.readIssues(ctx, opts, results)
		case "attachments":
			err = s.readAttachments(ctx, opts, results)
		case "comments":
			err = s.readComments(ctx, opts, results)
		case "cycles":
			err = s.readCycles(ctx, opts, results)
		case "documents":
			err = s.readDocuments(ctx, opts, results)
		case "external_users":
			err = s.readExternalUsers(ctx, opts, results)
		case "initiatives":
			err = s.readInitiatives(ctx, opts, results)
		case "initiative_to_projects":
			err = s.readInitiativeToProjects(ctx, opts, results)
		case "project_milestones":
			err = s.readProjectMilestones(ctx, opts, results)
		case "project_statuses":
			err = s.readProjectStatuses(ctx, opts, results)
		case "integrations":
			err = s.readIntegrations(ctx, opts, results)
		case "issue_labels":
			err = s.readIssueLabels(ctx, opts, results)
		case "organization":
			err = s.readOrganization(ctx, opts, results)
		case "project_updates":
			err = s.readProjectUpdates(ctx, opts, results)
		case "team_memberships":
			err = s.readTeamMemberships(ctx, opts, results)
		case "users":
			err = s.readUsers(ctx, opts, results)
		case "workflow_states":
			err = s.readWorkflowStates(ctx, opts, results)
		case "projects":
			err = s.readProjects(ctx, opts, results)
		case "teams":
			err = s.readTeams(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
}

func (s *LinearSource) executeGraphQL(ctx context.Context, query string, variables map[string]interface{}) (json.RawMessage, error) {
	reqBody := graphQLRequest{
		Query:     query,
		Variables: variables,
	}

	config.Debug("[LINEAR] Executing GraphQL query")

	var resp graphQLResponse
	httpResp, err := s.client.R(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(reqBody).
		SetResult(&resp).
		Post("")
	if err != nil {
		return nil, fmt.Errorf("graphql request failed: %w", err)
	}

	if !httpResp.IsSuccess() {
		return nil, fmt.Errorf("graphql request failed with status %d: %s", httpResp.StatusCode(), httpResp.String())
	}

	if len(resp.Errors) > 0 {
		var errMsgs []string
		for _, e := range resp.Errors {
			errMsgs = append(errMsgs, e.Message)
		}
		return nil, fmt.Errorf("graphql errors: %s", strings.Join(errMsgs, "; "))
	}

	return resp.Data, nil
}

func (s *LinearSource) readIssues(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading issues")
	return s.paginateAndSend(ctx, opts, results, issuesQuery, "issues", pageSize, issueFields, normalizeDictionaries)
}

func (s *LinearSource) readAttachments(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading attachments")
	return s.paginateAndSend(ctx, opts, results, attachmentsQuery, "attachments", pageSize, attachmentFields, normalizeDictionaries)
}

func (s *LinearSource) readComments(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading comments")
	return s.paginateAndSend(ctx, opts, results, commentsQuery, "comments", pageSize, commentFields, normalizeDictionaries)
}

func (s *LinearSource) readCycles(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading cycles")
	return s.paginateAndSend(ctx, opts, results, cyclesQuery, "cycles", pageSize, cycleFields, normalizeDictionaries)
}

func (s *LinearSource) readDocuments(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading documents")
	return s.paginateAndSend(ctx, opts, results, documentsQuery, "documents", pageSize, documentFields, normalizeDictionaries)
}

func (s *LinearSource) readExternalUsers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading external_users")
	return s.paginateAndSend(ctx, opts, results, externalUsersQuery, "externalUsers", pageSize, externalUserFields, normalizeDictionaries)
}

func (s *LinearSource) readInitiatives(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading initiatives")
	return s.paginateAndSend(ctx, opts, results, initiativesQuery, "initiatives", pageSize, initiativeFields, normalizeDictionaries)
}

func (s *LinearSource) readInitiativeToProjects(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading initiative_to_projects")
	return s.paginateAndSend(ctx, opts, results, initiativeToProjectsQuery, "initiativeToProjects", pageSize, initiativeToProjectFields, normalizeDictionaries)
}

func (s *LinearSource) readProjectMilestones(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading project_milestones")
	return s.paginateAndSend(ctx, opts, results, projectMilestonesQuery, "projectMilestones", pageSize, projectMilestoneFields, normalizeDictionaries)
}

func (s *LinearSource) readProjectStatuses(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading project_statuses")
	return s.paginateAndSend(ctx, opts, results, projectStatusesQuery, "projectStatuses", pageSize, projectStatusFields, normalizeDictionaries)
}

func (s *LinearSource) readIntegrations(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading integrations")
	return s.paginateAndSend(ctx, opts, results, integrationsQuery, "integrations", pageSize, integrationFields, normalizeDictionaries)
}

func (s *LinearSource) readIssueLabels(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading issue_labels")
	return s.paginateAndSend(ctx, opts, results, issueLabelsQuery, "issueLabels", pageSize, issueLabelFields, normalizeDictionaries)
}

func (s *LinearSource) readOrganization(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[LINEAR] Reading organization")

	// Organization is a special case - no pagination
	data, err := s.executeGraphQL(ctx, organizationQuery, nil)
	if err != nil {
		return fmt.Errorf("failed to execute GraphQL query: %w", err)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(data, &response); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	viewer, ok := response["viewer"].(map[string]interface{})
	if !ok {
		return nil
	}

	org, ok := viewer["organization"].(map[string]interface{})
	if !ok {
		return nil
	}

	transformed := normalizeDictionaries(org)
	items := []map[string]interface{}{transformed}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, organizationFields, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to build arrow record: %w", err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[LINEAR] Sent 1 organization record")
	return nil
}

func (s *LinearSource) readProjectUpdates(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading project_updates")
	return s.paginateAndSend(ctx, opts, results, projectUpdatesQuery, "projectUpdates", pageSize, projectUpdateFields, normalizeDictionaries)
}

func (s *LinearSource) readTeamMemberships(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading team_memberships")
	return s.paginateAndSend(ctx, opts, results, teamMembershipsQuery, "teamMemberships", pageSize, teamMembershipFields, normalizeDictionaries)
}

func (s *LinearSource) readUsers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading users")
	return s.paginateAndSend(ctx, opts, results, usersQuery, "users", pageSize, userFields, normalizeDictionaries)
}

func (s *LinearSource) readWorkflowStates(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading workflow_states")
	return s.paginateAndSend(ctx, opts, results, workflowStatesQuery, "workflowStates", pageSize, workflowStateFields, normalizeDictionaries)
}

func (s *LinearSource) readProjects(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading projects")
	return s.paginateAndSend(ctx, opts, results, projectsQuery, "projects", pageSize, projectFields, normalizeDictionaries)
}

func (s *LinearSource) readTeams(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	config.Debug("[LINEAR] Reading teams")
	return s.paginateAndSend(ctx, opts, results, teamsQuery, "teams", pageSize, teamFields, normalizeDictionaries)
}

const issuesQuery = `
query Issues($cursor: String, $filter: IssueFilter) {
  issues(first: 50, after: $cursor, filter: $filter) {
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
`

const attachmentsQuery = `
query Attachments($cursor: String, $filter: AttachmentFilter) {
  attachments(first: 50, after: $cursor, filter: $filter) {
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
`

const commentsQuery = `
query Comments($cursor: String, $filter: CommentFilter) {
  comments(first: 50, after: $cursor, filter: $filter) {
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
`

const cyclesQuery = `
query Cycles($cursor: String, $filter: CycleFilter) {
  cycles(first: 50, after: $cursor, filter: $filter) {
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
`

const documentsQuery = `
query Documents($cursor: String, $filter: DocumentFilter) {
  documents(first: 50, after: $cursor, filter: $filter) {
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
`

const externalUsersQuery = `
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
`

const initiativesQuery = `
query Initiatives($cursor: String, $filter: InitiativeFilter) {
  initiatives(first: 50, after: $cursor, filter: $filter) {
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
`

const initiativeToProjectsQuery = `
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
`

const projectMilestonesQuery = `
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
`

const projectStatusesQuery = `
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
`

const integrationsQuery = `
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
`

const issueLabelsQuery = `
query IssueLabels($cursor: String, $filter: IssueLabelFilter) {
  issueLabels(first: 50, after: $cursor, filter: $filter) {
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
`

const organizationQuery = `
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
      periodUploadVolume
      previousUrlKeys
      roadmapEnabled
      samlEnabled
      scimEnabled
    }
  }
}
`

const projectUpdatesQuery = `
query ProjectUpdates($cursor: String, $filter: ProjectUpdateFilter) {
  projectUpdates(first: 50, after: $cursor, filter: $filter) {
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
`

const teamMembershipsQuery = `
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
`

const usersQuery = `
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
`

const workflowStatesQuery = `
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
`

const projectsQuery = `
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
`

const teamsQuery = `
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
`

// buildIncrementalFilter creates a Linear API filter for incremental loading
// Only used for tables that support server-side filtering
func buildIncrementalFilter(tableName string, opts source.ReadOptions) map[string]interface{} {
	// Check if table supports filtering
	if !tablesWithFilterSupport[tableName] {
		return nil
	}

	// Only build filter if IntervalStart or IntervalEnd is set
	if opts.IntervalStart == nil && opts.IntervalEnd == nil {
		return nil
	}

	filter := make(map[string]interface{})
	updatedAtFilter := make(map[string]interface{})

	// Add gte (greater than or equal) filter if IntervalStart is set
	if opts.IntervalStart != nil {
		updatedAtFilter["gte"] = opts.IntervalStart.Format(time.RFC3339)
		config.Debug("[LINEAR] Filter: updatedAt >= %s", opts.IntervalStart.Format(time.RFC3339))
	}

	if opts.IntervalEnd != nil {
		updatedAtFilter["lte"] = opts.IntervalEnd.Format(time.RFC3339)
		config.Debug("[LINEAR] Filter: updatedAt <= %s", opts.IntervalEnd.Format(time.RFC3339))
	}

	if len(updatedAtFilter) > 0 {
		filter["updatedAt"] = updatedAtFilter
		return filter
	}

	return nil
}

func filterByUpdatedAt(items []map[string]interface{}, start, end *time.Time) []map[string]interface{} {
	if start == nil && end == nil {
		return items
	}

	var filtered []map[string]interface{}
	for _, item := range items {
		updatedAt, ok := item["updatedAt"].(string)
		if !ok {
			filtered = append(filtered, item)
			continue
		}

		t, err := time.Parse(time.RFC3339, updatedAt)
		if err != nil {
			filtered = append(filtered, item)
			continue
		}

		if start != nil && t.Before(*start) {
			continue
		}
		if end != nil && t.After(*end) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

// paginateAndSend fetches all pages of data using cursor-based pagination and sends batches
func (s *LinearSource) paginateAndSend(
	ctx context.Context,
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
	query string,
	queryField string,
	pageSize int,
	fields []schema.Column,
	transformFn func(map[string]interface{}) map[string]interface{},
) error {
	cursor := ""
	totalRecords := 0

	// Build incremental filter (only for tables that support it)
	filter := buildIncrementalFilter(queryField, opts)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		variables := map[string]interface{}{}
		if cursor != "" {
			variables["cursor"] = cursor
		}
		if len(filter) > 0 {
			variables["filter"] = filter
		}

		data, err := s.executeGraphQL(ctx, query, variables)
		if err != nil {
			return fmt.Errorf("failed to execute GraphQL query: %w", err)
		}

		var response map[string]interface{}
		if err := json.Unmarshal(data, &response); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}

		queryData, ok := response[queryField].(map[string]interface{})
		if !ok {
			break
		}

		nodes, ok := queryData["nodes"].([]interface{})
		if !ok || len(nodes) == 0 {
			break
		}

		// Transform all nodes
		var items []map[string]interface{}
		for _, node := range nodes {
			if nodeMap, ok := node.(map[string]interface{}); ok {
				transformed := transformFn(nodeMap)
				items = append(items, transformed)
			}
		}

		// Client-side filtering for tables that don't support server-side filter
		if !tablesWithFilterSupport[queryField] && (opts.IntervalStart != nil || opts.IntervalEnd != nil) {
			items = filterByUpdatedAt(items, opts.IntervalStart, opts.IntervalEnd)
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, fields, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record: %w", err)
			}
			results <- source.RecordBatchResult{Batch: record}
			totalRecords += len(items)
			config.Debug("[LINEAR] Sent %d records (total: %d)", len(items), totalRecords)
		}

		// Check pagination
		pageInfo, ok := queryData["pageInfo"].(map[string]interface{})
		if !ok {
			break
		}

		hasNextPage, _ := pageInfo["hasNextPage"].(bool)
		if !hasNextPage {
			break
		}

		endCursor, ok := pageInfo["endCursor"].(string)
		if !ok || endCursor == "" {
			break
		}
		cursor = endCursor
	}

	config.Debug("[LINEAR] Finished reading %s: %d total records", queryField, totalRecords)
	return nil
}

// normalizeDictionaries flattens nested objects:
// {"creator": {"id": "123"}} -> {"creatorId": "123"}
// {"labels": {"nodes": [...]}} -> {"labels": [...]}
func normalizeDictionaries(item map[string]interface{}) map[string]interface{} {
	normalized := make(map[string]interface{})

	for key, value := range item {
		if valueMap, ok := value.(map[string]interface{}); ok {
			if id, hasID := valueMap["id"]; hasID {
				normalized[key+"Id"] = id
			} else if nodes, hasNodes := valueMap["nodes"]; hasNodes {
				normalized[key] = nodes
			} else {
				normalized[key] = value
			}
		} else {
			normalized[key] = value
		}
	}

	return normalized
}
