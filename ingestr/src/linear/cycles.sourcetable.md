# Linear Cycles Source Table

## cycles

A set of issues to be resolved in a specified amount of time.

### Fields

- `id` (ID!) - The unique identifier of the entity
- `archived_at` (DateTime) - The time at which the entity was archived. Null if the entity has not been archived
- `auto_archived_at` (DateTime) - The time at which the cycle was automatically archived by the auto pruning process
- `completed_at` (DateTime) - The completion time of the cycle. If null, the cycle hasn't been completed
- `completed_issue_count_history` ([Float!]!) - The number of completed issues in the cycle after each day
- `completed_scope_history` ([Float!]!) - The number of completed estimation points after each day
- `created_at` (DateTime!) - The time at which the entity was created
- `current_progress` (JSONObject!) - [Internal] The current progress of the cycle
- `description` (String) - The cycle's description
- `ends_at` (DateTime!) - The end time of the cycle
- `in_progress_scope_history` ([Float!]!) - The number of in progress estimation points after each day
- `inherited_from_id` (String) - The ID of the cycle inherited from
- `is_active` (Boolean!) - Whether the cycle is currently active
- `is_future` (Boolean!) - Whether the cycle is in the future
- `is_next` (Boolean!) - Whether the cycle is the next cycle for the team
- `is_past` (Boolean!) - Whether the cycle is in the past
- `is_previous` (Boolean!) - Whether the cycle is the previous cycle for the team
- `issue_count_history` ([Float!]!) - The total number of issues in the cycle after each day
- `name` (String) - The custom name of the cycle
- `number` (Float!) - The number of the cycle
- `progress` (Float!) - The overall progress of the cycle. This is the (completed estimate points + 0.25 * in progress estimate points) / total estimate points
- `progress_history` (JSONObject!) - [Internal] The progress history of the cycle
- `scope_history` ([Float!]!) - The total number of estimation points after each day
- `starts_at` (DateTime!) - The start time of the cycle
- `team_id` (String!) - The ID of the team that the cycle is associated with
- `updated_at` (DateTime!) - The last time at which the entity was meaningfully updated. This is the same as the creation time if the entity hasn't been updated after creation

### Foreign Keys

- `team_id` -> `teams.id` - References the team that owns this cycle
- `inherited_from_id` -> `cycles.id` - References the parent cycle if this cycle was inherited from another