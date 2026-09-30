# Reverse-ETL destination test checklist

Run every row that applies to the destination, live, against a real account. A row marked *(if …)* applies only when the API has that feature; mark it N/A with the reason otherwise.

Each row passes only when all three agree:
1. the exit code (0, or non-zero for an expected failure),
2. the expected text in the output (reject codes, keys, warnings),
3. an independent read of the remote state through the API or UI, not ingestr's own success message.

A row expected to fail before writing also checks that none of its keys exist remotely afterwards.

A row whose expected result says *Document* passes when the finding is written into the destination's docs page, and into SKILL.md's "Verify live" list if it's a new kind of API behaviour.

Terms used below:
- **unique field**: a field the API enforces as unique and can upsert on (an external id, a custom unique attribute).
- **record id**: the id the platform assigns to each record.
- **lookup field**: a case-insensitive unique field such as email or domain.
- **parent/child**: two objects where the child links to the parent.

## 0. Setup

| ID | Case | Expected |
|---|---|---|
| 0.01 | Start state is known: only records the vendor seeds and can't remove | Counts match the documented start state |
| 0.02 | Source tables come from a warehouse (not CSV), created by one setup script | Arrow types (decimal, date, timestamp, bool, list, JSON, NaN floats) reach the destination |
| 0.03 | Binary under test is the current build | `make build` after every fix, then re-run the whole checklist |
| 0.04 | Secrets are redacted from every saved output and report | Grep the artifacts (and `unzip -p` any xlsx) for the raw secrets: no match |

## 1. Seeding and idempotency

| ID | Case | Expected |
|---|---|---|
| 1.01 | merge parent records by a unique field, with typed values (number, currency, date, select, multi-select) | Records created; every typed value reads back correctly |
| 1.02 | Re-run the same merge | Record count unchanged; no duplicates |
| 1.03 | Self-reference: child records link to a parent of the same object by the parent's unique field | Links set to the right parent |
| 1.04 | Child records linked to parents by the parent's unique field (date, phone, text fields) | Each child points to the right parent |
| 1.05 | A second object type with a status/stage field | Records created with the right status |
| 1.06 | Records linked to parents with currency + date values | Values and links read back correctly |
| 1.07 | A record linked to two different parents (e.g. a person and a company) in one row | Both links set |
| 1.08 | A second child object linked to the parent | Links set |
| 1.09 | Many-to-many: a junction object *(if the API has one)* or a multi-value link *(if the API has one)* | Every pair linked; re-run is a no-op |
| 1.10 | A custom object with every field type the API has: text, long text, number, currency, percent/rating, date, timestamp, checkbox, select, multi-select, email, phone, url, location/JSON, user, link | Every value reads back as sent, within the API's precision (note it, e.g. whole seconds or milliseconds) |
| 1.11 | An object that has no unique field *(if any)* | Only append/update/delete work; merge/replace are refused before writing |

## 2. Strategies × match fields

| ID | Case | Expected |
|---|---|---|
| 2.01 | merge: one row updates an existing record, one creates a new one | Update and create both land |
| 2.02 | append: always creates (no match) | New records created |
| 2.03 | append re-run, on a table where no unique field is filled | Duplicates created (expected, documented) |
| 2.04 | append with a unique field filled: `--primary-key` only labels rejects | Records created; warning that append never matches |
| 2.05 | append re-run onto existing unique values | Each row rejected per row, named by its key, with a hint to use merge; no duplicates |
| 2.06 | append with the match-field dest-table param | Refused before writing |
| 2.07 | update by a lookup field, upper-cased source value; unknown value | Matched case-insensitively and updated; unknown value NOT_FOUND; nothing created |
| 2.08 | update by a non-unique field | Every matching record updated |
| 2.09 | update by a unique field: a row with a null key; an unknown key | Null key skipped with a warning; unknown key NOT_FOUND; nothing created |
| 2.10 | Read the destination back into the warehouse through ingestr's source for that platform *(if one exists; otherwise the vendor API)* | Snapshot tables hold the record ids |
| 2.11 | update by record id (the default match: no match param, no `--primary-key`) | Records updated |
| 2.12 | update by record id in another form: short-form id *(if the API has them)*, upper-cased id | Records updated where ids are case-insensitive; document whether they are (some short-form ids are case-sensitive) |
| 2.13 | update by a lookup/reference field; a malformed id | Matching records updated, or refused before writing if the API can't match on references (document which); malformed id rejected per row |
| 2.14 | delete by a unique field; unknown key | Record deleted; unknown key NOT_FOUND |
| 2.15 | delete by record id (ids from the snapshot) | Records deleted |
| 2.16 | delete a record that others still link to | The API refuses per row, or deletes and drops the links; document which |
| 2.17 | update a second object type by a unique or non-unique field | Records updated |
| 2.18 | delete a second object type by a unique or non-unique field | Records deleted |
| 2.19 | update status/stage and amount on one record (include a status title with an emoji or non-ASCII text) | Both values updated |
| 2.20 | Close/finish a record through its status | Status set |
| 2.21 | Junction/multi-value link: remove every link of one parent (delete junction rows, or a null list) | Links removed; the linked records stay |
| 2.22 | Several source rows reach one record through different values (two emails of one person) or a repeated key/id, with delete | Record deleted once; no NOT_FOUND for the later rows |
| 2.23 | A later batch reaches a record already deleted by an earlier batch through another value | Not rejected |
| 2.24 | merge by the second value of a multi-value unique field | Same record matched; its other values kept |
| 2.25 | Same values sent twice (no-op update) | Document whether the API bumps the modified timestamp or fires workflows |
| 2.26 | Upsert onto a soft-deleted record *(if the API soft-deletes)* | Document: restored, new duplicate, or error |
| 2.27 | append with `--interval-start`/`--interval-end` on an incremental key | Only rows in the window are created. `--incremental-key` alone keeps no state: a re-run creates duplicates |

## 3. Nulls

| ID | Case | Expected |
|---|---|---|
| 3.01 | `--write-nulls=false`: nulls in some fields | Null fields keep their values; non-null fields updated |
| 3.02 | Default (write-nulls on): nulls clear the field | Fields empty remotely |
| 3.03 | Restore the values | Values back |
| 3.04 | `write_nulls` as a dest-table param | Refused as unknown before writing |
| 3.05 | Empty string vs null *(if the API distinguishes them)* | Document what each does |
| 3.06 | Null in a multi-value field (default and `--write-nulls=false`) | Cleared / kept |
| 3.07 | *(if the API has composite values)* Composite value (a name made of parts), all parts null, default | Value cleared |
| 3.08 | *(if the API has composite values)* Composite value, all parts null, `--write-nulls=false` | Value unchanged |
| 3.09 | *(if the API has composite values)* Composite value with some parts null or missing (default and `--write-nulls=false`) | Written as the new value; missing parts empty; never mixed with stored parts |
| 3.10 | *(if the API has composite values)* Composite value sent as one string in each accepted form (e.g. `Last, First` and without a comma) | Document how it's split |

## 4. Reject modes and per-row failures

Use one source table with good rows and several kinds of bad row: bad format, unknown parent, too long or invalid value, plus a good create.

| ID | Case | Expected |
|---|---|---|
| 4.01 | `--reject-mode fail` (default) | Good rows land; exit non-zero; every reject listed with its code and key |
| 4.02 | `--reject-mode skip` | Same rows land; exit 0; rejects reported |
| 4.03 | `--reject-mode fail_fast` | No new request starts after the first reject; a remote read shows later batches were never sent (requests already in flight may land) |
| 4.04 | Same key twice in one run | Document the API's behaviour (both rejected, or one record, last row wins) and check the hint |
| 4.05 | A required field missing on create | Rejected per row; message names the field |
| 4.06 | A list sent to a single-value field | Rejected per row (or refused before writing) |
| 4.07 | Unknown select option and unknown status title | Rejected per row |
| 4.08 | Reject messages that carry internal field ids | The field's name is shown next to the id |
| 4.09 | An error that fails every row alike (quota, plan limit, missing scope) | Run aborts once, whatever the reject mode; not one reject per row |
| 4.10 | A 429 or temporary 5xx in the middle of a run | 429 retried; 5xx retried only on idempotent requests (update, upsert, delete). A 5xx on a create is reported, never resent, so it can't duplicate |
| 4.11 | *(if requests are batched)* A batch with one bad row | Good rows in that batch land (bisection or per-record results); only the bad row is rejected |

## 5. Relationships

| ID | Case | Expected |
|---|---|---|
| 5.01 | Relink a child to another parent; another row's null link with `--write-nulls=false` | Relinked; the null row keeps its link |
| 5.02 | Null link with default write-nulls | Link removed |
| 5.03 | Link by the parent's raw record id | Linked |
| 5.04 | Link through a user/owner field by email or username | Linked to that user; an unknown user is rejected per row |
| 5.05 | Link that can point to several object types, by record id *(if the API has them)* | Linked to the right object |
| 5.06 | Same, by a unique field with the object named in the column | Linked |
| 5.07 | Same, without the object named | Refused before writing, listing the objects to choose from |
| 5.08 | Polymorphic owner defaulting to one object type *(if the API has it)* | Linked without naming the object |
| 5.09 | `--columns` maps a plain source column onto a dotted link column | Linked |
| 5.10 | One row fills two object types for one link | Rejected per row, or both linked if the link holds several; document which |
| 5.11 | Link through a non-unique field on the parent | Refused before writing |
| 5.12 | Dotted columns with mixed-case object names (`link.People.email`) | Resolved to the API's own spelling |
| 5.13 | Multi-value link with a list value | Every item linked; document whether the list replaces or adds to current links |

### Associations *(if links are a separate API)*

| ID | Case | Expected |
|---|---|---|
| 5.14 | Link two records by their keys (positional: side 1 then side 2) | Linked; re-run is a no-op |
| 5.15 | Labelled/typed link; unknown label | Labelled link created; unknown label refused |
| 5.16 | Unlink the listed pairs | Only those links removed; records stay |
| 5.17 | Mirror links per from-record | Source links added, others for those records removed |
| 5.18 | Many-to-many through several rows | Every pair linked |
| 5.19 | Array key column | Cartesian product linked, duplicates removed |
| 5.20 | update/append on associations | Refused before writing |

## 6. Column mapping and naming

| ID | Case | Expected |
|---|---|---|
| 6.01 | `--columns dest::source` rename; `--primary-key` names the renamed column | Written to the renamed fields |
| 6.02 | `--primary-key` names the pre-rename column | Same record matched (the pipeline renames the key too) |
| 6.03 | `--columns` with a type | Refused (rename only) |
| 6.04 | `--schema-naming snake_case` *(if the destination sends names as written)* | Ignored with a warning; names sent as written. Otherwise the naming applies as for any destination |
| 6.05 | Upper/mixed-case source column names | Resolved to the API's field names |
| 6.06 | Source carries ingestr's own columns (`_ingestr_loaded_at`, `_ingestr_run_id`) and a round-tripped record id column | Not sent as values; no "unknown field" error |

## 7. Numeric keys and values

| ID | Case | Expected |
|---|---|---|
| 7.01 | merge on a numeric unique field: `1001.00` meets a stored `1001`; a new number | Matched; new record created |
| 7.02 | update by the number; unknown number | Updated; NOT_FOUND |
| 7.03 | NaN / +Inf / -Inf floats | Treated as null (cleared or skipped per write-nulls); never sent as text |
| 7.04 | Bad values in one batch: unknown select option, unparseable date, text in a number | Bad rows rejected per row; good row lands |
| 7.05 | A read-only or formula field in the source | Refused before writing |
| 7.06 | Numeric key with many decimals (`1.0000000000000000001`) and negatives | Sent exactly, as a valid number |
| 7.07 | Timezones: a timestamp with time zone vs a naive timestamp, and dates near midnight UTC | Stored instant and date match the source; the naive case is documented |
| 7.08 | Multi-select: a list value, and the API's append syntax *(if it has one)* | List replaces current values; append syntax adds; document both |

## 8. replace (mirror)

| ID | Case | Expected |
|---|---|---|
| 8.01 | replace with v1 | The object holds exactly the source's records |
| 8.02 | replace with v2: one update, one create, the rest removed | Exactly v2 |
| 8.03 | Empty source | Sweep skipped with a warning; nothing removed |
| 8.04 | A rejected row under `--reject-mode fail` | Sweep skipped; run fails; nothing removed |
| 8.05 | Same under `--reject-mode skip` | Sweep runs; rejected row reported |
| 8.06 | A row with no key | Created, and survives its own sweep |
| 8.07 | Every row rejected under `skip` | Sweep skipped with a warning |
| 8.08 | Case-sensitive unique field: source has `A-1`, a stale `a-1` exists | `a-1` removed, `A-1` kept |
| 8.09 | Metadata/describe call fails | Stops before writing if the metadata decides how keys compare (a mirror sweep could delete kept records); otherwise warns and skips column validation |

## 9. Alternate write path *(if the API has a bulk/async path)*

| ID | Case | Expected |
|---|---|---|
| 9.01 | Bulk merge: create | Records created in one job |
| 9.02 | Bulk merge: update, clear a field, unlink, create | All four land |
| 9.03 | Bulk update by a lookup field; unknown value | Updated; NOT_FOUND |
| 9.04 | Bulk with rejected rows | Rejects attributed to the right keys (results may be unordered) |
| 9.05 | Bulk + `fail_fast` | Refused before any job is created |
| 9.06 | The default load method + `fail_fast` | Refused if the default is bulk |
| 9.07 | Bulk delete; unknown key | Deleted; NOT_FOUND |
| 9.08 | Bulk replace | Exact mirror |
| 9.09 | Bulk append | Records created |
| 9.10 | A reserved value in the bulk format (e.g. a "clear" token) sent as literal text | Rejected, not silently clearing the field |

## 10. Volume

| ID | Case | Expected |
|---|---|---|
| 10.01 | merge more rows than one request holds (several requests' worth) | All created; record wall time |
| 10.02 | merge the same rows with a new value | All updated |
| 10.03 | delete them all | 0 left |
| 10.04 | Several batches in flight at once (destination parallelism above 1) | No lost or duplicated writes |
| 10.05 | Same on the alternate path *(if any)*, long enough to exercise job polling | All land; record wall time |

## 11. Pre-flight validation

Every row must fail before any write.

| ID | Case | Expected |
|---|---|---|
| 11.01 | No `--incremental-strategy` | Refused if the default would be destructive; otherwise the documented default is used |
| 11.02 | `delete+insert` | Refused |
| 11.03 | `scd2` | Refused |
| 11.04 | merge without the match-field param | Refused |
| 11.05 | merge on the record id | Refused (ids are server-assigned) |
| 11.06 | replace without the match-field param | Refused |
| 11.07 | merge without `--primary-key` | Refused |
| 11.08 | Composite `--primary-key` | Refused |
| 11.09 | Match field that isn't unique, for merge | Refused, listing the unique fields |
| 11.10 | Match field that doesn't exist | Refused |
| 11.11 | Unknown object | Refused, listing the available objects |
| 11.12 | Display label instead of the API name; a label that collides with another object's | Refused with a "did you mean" hint |
| 11.13 | Source column that isn't a field | Refused, naming the column |
| 11.14 | Read-only field | Refused |
| 11.15 | Unknown relationship in a dotted column | Refused |
| 11.16 | Unknown dest-table param (including another destination's param name) | Refused |
| 11.17 | Invalid `--reject-mode` | Refused |
| 11.18 | `--primary-key` column missing from the source | Refused |
| 11.19 | update with the default record-id match but no record-id column | Refused |
| 11.20 | A strategy a table doesn't accept *(if tables allow only some strategies)* | Refused before sending |
| 11.21 | No strategy on such a table | The table's own strategy is used |
| 11.22 | `--full-refresh` with any strategy | Refused before writing: there's no table to rebuild, and it must never turn into a mirror sweep. *Today it runs `replace`; the refusal is a planned change* |
| 11.23 | `--full-refresh` on a table with a fixed strategy | Refused before writing |

## 12. Auth and URI

| ID | Case | Expected |
|---|---|---|
| 12.01 | Every supported auth method, including a pre-minted access token | Connects |
| 12.02 | An explicit older API version *(if selectable)* | Connects |
| 12.03 | Wrong secret / unknown key | Fails at connect, naming the problem |
| 12.04 | Invalid or revoked token | Fails at connect |
| 12.05 | Missing required URI part (domain, key) | Fails immediately |
| 12.06 | Unknown auth method | Fails immediately |
| 12.07 | Invalid load method *(if any)* | Fails immediately |
| 12.08 | API version that doesn't exist *(if selectable)* | Fails at connect |
| 12.09 | Password flow without a password *(if any)* | Fails immediately |
| 12.10 | Token without write scope, or without schema-read scope | Fails at connect or pre-flight, naming the missing scope |

## 13. Cleanup

| ID | Case | Expected |
|---|---|---|
| 13.01 | Snapshot every object through ingestr's source *(if one exists; otherwise the vendor API)* | Snapshot tables refreshed |
| 13.02 | Delete children before parents, object by object, with ingestr itself | Each object back to its start count |
| 13.03 | Empty the recycle bin *(if the API has one and it counts toward storage)* | Storage freed |
| 13.04 | Confirm the start state | Counts match 0.01 |

## 14. Use-case scenarios

Real-world flows to run end to end once the rows above pass.

| ID | Scenario | Strategy and match | Expected |
|---|---|---|---|
| 14.01 | Score each person (lead score) from the warehouse | update by lookup field | Each person's score matches the warehouse; unknown people rejected |
| 14.02 | Last product activity per person | update by record id | Activity fields match the warehouse; unknown ids rejected |
| 14.03 | Flag every person of an account at once | update by a non-unique field (every match) | Every person of the account is flagged; people of other accounts untouched |
| 14.04 | Load new signups without duplicating existing people | merge by lookup field | New people created, existing ones updated; no duplicates on re-run |
| 14.05 | Remove a person on a data-deletion request | delete by lookup field; document permanent vs soft delete | Records gone (or soft-deleted, as documented); a re-run rejects nothing new |
| 14.06 | Enrich companies (industry, size, revenue) | merge by a custom unique field | Company fields match the warehouse; re-run creates nothing |
| 14.07 | Plan/contract tier per company | update by a custom unique field | Tier set on existing companies; unknown keys rejected, nothing created |
| 14.08 | Move tickets/records from another system | merge by a custom unique field | Every source record exists once remotely; re-run creates nothing |
| 14.09 | Sync a product catalog (sku, name, price) | merge; replace for an exact catalog | Catalog matches the warehouse; with replace, products not in the source are removed |
| 14.10 | Orders or purchases against people | merge by order id, linked to the person | Each order exists once and links to the right person |
| 14.11 | Custom objects (subscriptions, assets, projects) by name and by id | merge by a custom unique field | Same result whether the object is named by name or by id |
| 14.12 | Link people to companies; with a role label; remove ended links; mirror links from the warehouse | link / labelled link / unlink / mirror | Links match the warehouse, labels included; unlink and mirror remove only links, never records |
| 14.13 | Many-to-many links, by rows and by an array key | link | Every pair linked once |
| 14.14 | Fill empty fields from the warehouse without clearing the rest | update with `--write-nulls=false` | Empty fields filled; fields that were null in the source keep their remote values |
| 14.15 | Hide inactive records, restorable later | delete (soft) or update a status; document which | Records hidden and restorable (or status set), as documented |
| 14.16 | One-time historical backfill | merge, one run | Every historical row exists once remotely |
| 14.17 | List/segment membership *(if supported)* | Document as supported or out of scope | Documented |
| 14.18 | Merge duplicate records into one *(if supported)* | Document as supported or out of scope | Documented |
| 14.19 | Event registration and attendance per person | merge by two keys (event + person), or document as out of scope | Documented, or registrations and attendance set remotely |
