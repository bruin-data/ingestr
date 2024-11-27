import datetime
from typing import Any, Dict, Iterable, Iterator, List, Literal, Optional, Union

from pyairtable.models._base import AirtableModel, update_forward_refs
from pyairtable.models.audit import AuditLogResponse
from pyairtable.models.schema import EnterpriseInfo, UserGroup, UserInfo
from pyairtable.utils import (
    cache_unless_forced,
    coerce_iso_str,
    coerce_list_str,
    enterprise_only,
)


@enterprise_only
class Enterprise:
    """
    Represents an Airtable enterprise account.

    >>> enterprise = api.enterprise("entUBq2RGdihxl3vU")
    >>> enterprise.info().workspace_ids
    ['wspmhESAta6clCCwF', ...]
    """

    def __init__(self, api: "pyairtable.api.api.Api", workspace_id: str):
        self.api = api
        self.id = workspace_id
        self._info: Optional[EnterpriseInfo] = None

    @property
    def url(self) -> str:
        return self.api.build_url("meta/enterpriseAccounts", self.id)

    @cache_unless_forced
    def info(self) -> EnterpriseInfo:
        """
        Retrieve basic information about the enterprise, caching the result.
        """
        params = {"include": ["collaborators", "inviteLinks"]}
        response = self.api.get(self.url, params=params)
        return EnterpriseInfo.from_api(response, self.api)

    def group(self, group_id: str, collaborations: bool = True) -> UserGroup:
        """
        Retrieve information on a single user group with the given ID.

        Args:
            group_id: A user group ID (``grpQBq2RGdihxl3vU``).
            collaborations: If ``False``, no collaboration data will be requested
                from Airtable. This may result in faster responses.
        """
        params = {"include": ["collaborations"] if collaborations else []}
        url = self.api.build_url(f"meta/groups/{group_id}")
        payload = self.api.get(url, params=params)
        return UserGroup.parse_obj(payload)

    def user(self, id_or_email: str, collaborations: bool = True) -> UserInfo:
        """
        Retrieve information on a single user with the given ID or email.

        Args:
            id_or_email: A user ID (``usrQBq2RGdihxl3vU``) or email address.
            collaborations: If ``False``, no collaboration data will be requested
                from Airtable. This may result in faster responses.
        """
        return self.users([id_or_email], collaborations=collaborations)[0]

    def users(
        self,
        ids_or_emails: Iterable[str],
        collaborations: bool = True,
    ) -> List[UserInfo]:
        """
        Retrieve information on the users with the given IDs or emails.

        Read more at `Get users by ID or email <https://airtable.com/developers/web/api/get-users-by-id-or-email>`__.

        Args:
            ids_or_emails: A sequence of user IDs (``usrQBq2RGdihxl3vU``)
                or email addresses (or both).
            collaborations: If ``False``, no collaboration data will be requested
                from Airtable. This may result in faster responses.
        """
        user_ids: List[str] = []
        emails: List[str] = []
        for value in ids_or_emails:
            (emails if "@" in value else user_ids).append(value)

        response = self.api.get(
            url=f"{self.url}/users",
            params={
                "id": user_ids,
                "email": emails,
                "include": ["collaborations"] if collaborations else [],
            },
        )
        # key by user ID to avoid returning duplicates
        users = {
            info.id: info
            for user_obj in response["users"]
            if (info := UserInfo.from_api(user_obj, self.api, context=self))
        }
        return list(users.values())

    def audit_log(
        self,
        *,
        page_size: Optional[int] = None,
        page_limit: Optional[int] = None,
        sort_asc: Optional[bool] = False,
        previous: Optional[str] = None,
        next: Optional[str] = None,
        start_time: Optional[Union[str, datetime.date, datetime.datetime]] = None,
        end_time: Optional[Union[str, datetime.date, datetime.datetime]] = None,
        user_id: Optional[Union[str, Iterable[str]]] = None,
        event_type: Optional[Union[str, Iterable[str]]] = None,
        model_id: Optional[Union[str, Iterable[str]]] = None,
        category: Optional[Union[str, Iterable[str]]] = None,
    ) -> Iterator[AuditLogResponse]:
        """
        Retrieve and yield results from the `Audit Log <https://airtable.com/developers/web/api/audit-logs-integration-guide>`__,
        one page of results at a time. Each result is an instance of :class:`~pyairtable.models.audit.AuditLogResponse`
        and contains the pagination IDs returned from the API, as described in the linked documentation.

        By default, the Airtable API will return up to 180 days of audit log events, going backwards from most recent.
        Retrieving all records may take some time, but is as straightforward as:

            >>> enterprise = Enterprise("entYourEnterpriseId")
            >>> events = [
            ...     event
            ...     for page in enterprise.audit_log()
            ...     for event in page.events
            ... ]

        If you are creating a record of all audit log events, you probably want to start with the earliest
        events in the retention window and iterate chronologically. You'll likely have a job running
        periodically in the background, so you'll need some way to persist the pagination IDs retrieved
        from the API in case that job is interrupted and needs to be restarted.

        The sample code below will use a local file to remember the next page's ID, so that if the job is
        interrupted, it will resume where it left off (potentially processing some entries twice).

        .. code-block:: python

            import os
            import shelve
            import pyairtable

            def handle_event(event):
                print(event)

            api = pyairtable.Api(os.environ["AIRTABLE_API_KEY"])
            enterprise = api.enterprise(os.environ["AIRTABLE_ENTERPRISE_ID"])
            persistence = shelve.open("audit_log.db")
            first_page = persistence.get("next", None)

            for page in enterprise.audit_log(sort_asc=True, next=first_page):
                for event in page.events:
                    handle_event(event)
                persistence["next"] = page.pagination.next

        For more information on any of the keyword parameters below, refer to the
        `audit log events <https://airtable.com/developers/web/api/audit-log-events>`__
        API documentation.

        Args:
            page_size: How many events per page to return (maximum 100).
            page_limit: How many pages to return before stopping.
            sort_asc: Whether to sort in ascending order (earliest to latest)
                rather than descending order (latest to earliest).
            previous: Requests the previous page of results from the given ID.
                See the `audit log integration guide <https://airtable.com/developers/web/api/audit-logs-integration-guide>`__
                for more information on pagination parameters.
            next: Requests the next page of results according to the given ID.
                See the `audit log integration guide <https://airtable.com/developers/web/api/audit-logs-integration-guide>`__
                for more information on pagination parameters.
            start_time: Earliest timestamp to retrieve (inclusive).
            end_time: Latest timestamp to retrieve (inclusive).
            originating_user_id: Retrieve audit log events originating
                from the provided user ID or IDs (maximum 100).
            event_type: Retrieve audit log events falling under the provided
                `audit log event type <https://airtable.com/developers/web/api/audit-log-event-types>`__
                or types (maximum 100).
            model_id: Retrieve audit log events taking action on, or involving,
                the provided model ID or IDs (maximum 100).
            category: Retrieve audit log events belonging to the provided
                audit log event category or categories.

        Returns:
            An object representing a single page of audit log results.
        """

        start_time = coerce_iso_str(start_time)
        end_time = coerce_iso_str(end_time)
        user_id = coerce_list_str(user_id)
        event_type = coerce_list_str(event_type)
        model_id = coerce_list_str(model_id)
        category = coerce_list_str(category)
        params = {
            "startTime": start_time,
            "endTime": end_time,
            "originatingUserId": user_id,
            "eventType": event_type,
            "modelId": model_id,
            "category": category,
            "pageSize": page_size,
            "sortOrder": ("ascending" if sort_asc else "descending"),
            "previous": previous,
            "next": next,
        }
        params = {k: v for (k, v) in params.items() if v}
        offset_field = "next" if sort_asc else "previous"
        url = self.api.build_url(f"meta/enterpriseAccounts/{self.id}/auditLogEvents")
        iter_requests = self.api.iterate_requests(
            method="GET",
            url=url,
            params=params,
            offset_field=offset_field,
        )
        for count, response in enumerate(iter_requests, start=1):
            parsed = AuditLogResponse.parse_obj(response)
            yield parsed
            if not parsed.events:
                return
            if page_limit is not None and count >= page_limit:
                return

    def remove_user(
        self,
        user_id: str,
        replacement: Optional[str] = None,
    ) -> "UserRemoved":
        """
        Unshare a user from all enterprise workspaces, bases, and interfaces.
        If applicable, the user will also be removed from as an enterprise admin.

        See `Remove user from enterprise <https://airtable.com/developers/web/api/remove-user-from-enterprise>`__
        for more information.

        Args:
            user_id: The user ID.
            replacement: If the user is the sole owner of any workspaces, you must
                specify a replacement user ID to be added as the new owner of such
                workspaces. If the user is not the sole owner of any workspaces,
                this is optional and will be ignored if provided.
        """
        url = f"{self.url}/users/{user_id}/remove"
        payload: Dict[str, Any] = {"isDryRun": False}
        if replacement:
            payload["replacementOwnerId"] = replacement
        response = self.api.post(url, json=payload)
        return UserRemoved.from_api(response, self.api, context=self)

    def claim_users(
        self, users: Dict[str, Literal["managed", "unmanaged"]]
    ) -> "ClaimUsersResponse":
        """
        Batch manage organizations enterprise account users. This endpoint allows you
        to change a user's membership status from being unmanaged to being an
        organization member, and vice versa.

        See `Manage user membership <https://airtable.com/developers/web/api/manage-user-membership>`__
        for more information.

        Args:
            users: A ``dict`` mapping user IDs or emails to the desired state,
                either ``"managed"`` or ``"unmanaged"``.
        """
        payload = {
            "users": [
                {
                    ("email" if "@" in key else "id"): key,
                    "state": value,
                }
                for (key, value) in users.items()
            ]
        }
        response = self.api.post(f"{self.url}/users/claim", json=payload)
        return ClaimUsersResponse.from_api(response, self.api, context=self)

    def delete_users(self, emails: Iterable[str]) -> "DeleteUsersResponse":
        """
        Delete multiple users by email.

        Args:
            emails: A list or other iterable of email addresses.
        """
        response = self.api.delete(f"{self.url}/users", params={"email": list(emails)})
        return DeleteUsersResponse.from_api(response, self.api, context=self)


class UserRemoved(AirtableModel):
    """
    Returned from the `Remove user from enterprise <https://airtable.com/developers/web/api/remove-user-from-enterprise>`__
    endpoint.
    """

    was_user_removed_as_admin: bool
    shared: "UserRemoved.Shared"
    unshared: "UserRemoved.Unshared"

    class Shared(AirtableModel):
        workspaces: List["UserRemoved.Shared.Workspace"]

        class Workspace(AirtableModel):
            permission_level: str
            workspace_id: str
            workspace_name: str
            user_id: str = ""

    class Unshared(AirtableModel):
        bases: List["UserRemoved.Unshared.Base"]
        interfaces: List["UserRemoved.Unshared.Interface"]
        workspaces: List["UserRemoved.Unshared.Workspace"]

        class Base(AirtableModel):
            user_id: str
            base_id: str
            base_name: str
            former_permission_level: str

        class Interface(AirtableModel):
            user_id: str
            base_id: str
            interface_id: str
            interface_name: str
            former_permission_level: str

        class Workspace(AirtableModel):
            user_id: str
            former_permission_level: str
            workspace_id: str
            workspace_name: str


class DeleteUsersResponse(AirtableModel):
    """
    Returned from the `Delete users by email <https://airtable.com/developers/web/api/delete-users-by-email>`__
    endpoint.
    """

    deleted_users: List["DeleteUsersResponse.UserInfo"]
    errors: List["DeleteUsersResponse.Error"]

    class UserInfo(AirtableModel):
        id: str
        email: str

    class Error(AirtableModel):
        type: str
        email: str
        message: Optional[str] = None


class ClaimUsersResponse(AirtableModel):
    """
    Returned from the `Manage user membership <https://airtable.com/developers/web/api/manage-user-membership>`__
    endpoint.
    """

    errors: List["ClaimUsersResponse.Error"]

    class Error(AirtableModel):
        id: Optional[str] = None
        email: Optional[str] = None
        type: str
        message: str


update_forward_refs(vars())


# These are at the bottom of the module to avoid circular imports
import pyairtable.api.api  # noqa
import pyairtable.api.base  # noqa
