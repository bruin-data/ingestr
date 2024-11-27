import base64
from functools import partial
from hmac import HMAC
from typing import Any, Callable, Dict, Iterator, List, Optional, Union

from typing_extensions import Self as SelfType

from pyairtable._compat import pydantic
from pyairtable.api.types import RecordId

from ._base import AirtableModel, CanDeleteModel, update_forward_refs

# Shortcuts to avoid lots of line wrapping
FD: Callable[[], Any] = partial(pydantic.Field, default_factory=dict)
FL: Callable[[], Any] = partial(pydantic.Field, default_factory=list)


class Webhook(CanDeleteModel, url="bases/{base.id}/webhooks/{self.id}"):
    """
    A webhook that has been retrieved from the Airtable API.

    >>> spec = {
    ...     "options": {
    ...         "filters": {
    ...             "dataTypes": ["tableData"],
    ...         }
    ...     }
    ... }
    >>> base.add_webhook("https://example.com", spec)
    CreateWebhookResponse(
        id='ach00000000000001',
        mac_secret_base64='c3VwZXIgZHVwZXIgc2VjcmV0',
        expiration_time='2023-07-01T00:00:00.000Z'
    )
    >>> webhooks = base.webhooks()
    >>> webhooks[0]
    Webhook(
        id='ach00000000000001',
        are_notifications_enabled=True,
        cursor_for_next_payload=1,
        is_hook_enabled=True,
        last_successful_notification_time=None,
        notification_url="https://example.com",
        last_notification_result=None,
        expiration_time="2023-07-01T00:00:00.000Z",
        specification: WebhookSpecification(...)
    )
    >>> webhooks[0].disable_notifications()
    >>> webhooks[0].enable_notifications()
    >>> webhooks[0].extend_expiration()
    >>> webhooks[0].delete()
    """

    id: str
    are_notifications_enabled: bool
    cursor_for_next_payload: int
    is_hook_enabled: bool
    last_successful_notification_time: Optional[str]
    notification_url: Optional[str]
    last_notification_result: Optional["WebhookNotificationResult"]
    expiration_time: Optional[str]
    specification: "WebhookSpecification"

    def enable_notifications(self) -> None:
        """
        Turn on notifications for this webhook.
        See `Enable/disable webhook notifications <https://airtable.com/developers/web/api/enable-disable-webhook-notifications>`_.
        """
        self._api.request(
            "POST", f"{self._url}/enableNotifications", json={"enable": True}
        )

    def disable_notifications(self) -> None:
        """
        Turn off notifications for this webhook.
        See `Enable/disable webhook notifications`_.
        """
        self._api.request(
            "POST", f"{self._url}/enableNotifications", json={"enable": False}
        )

    def extend_expiration(self) -> None:
        """
        Extend the life of a webhook by seven days.
        See `Refresh a webhook <https://airtable.com/developers/web/api/refresh-a-webhook>`_.
        """
        response = self._api.request("POST", f"{self._url}/refresh")
        self.expiration_time = response.get("expirationTime")

    def payloads(
        self, cursor: int = 1, *, limit: Optional[int] = None
    ) -> Iterator["WebhookPayload"]:
        """
        Iterate through all payloads on or after the given cursor.
        See :class:`~pyairtable.models.WebhookPayload`. Each payload will
        contain an extra attribute, ``cursor``, which you will need to store
        if you want to later resume retrieving payloads after that point.

        For more details on the mechanisms of retrieving webhook payloads,
        or to find more information about the data structures you'll get back,
        see `List webhook payloads <https://airtable.com/developers/web/api/list-webhook-payloads>`_.

        Args:
            cursor: The cursor of the first webhook payload to retrieve.
            limit: The number of payloads to yield before stopping.
                If not provided, will retrieve all remaining payloads.

        Usage:
            >>> webhook = Base.webhook("ach00000000000001")
            >>> iter_payloads = webhook.payloads()
            >>> next(iter_payloads)
            WebhookPayload(
                timestamp="2022-02-01T21:25:05.663Z",
                base_transaction_number=4,
                payload_format="v0",
                action_metadata=ActionMetadata(
                    source="client",
                    source_metadata={
                        "user": {
                            "id": "usr00000000000000",
                            "email": "foo@bar.com",
                            "permissionLevel": "create"
                        }
                    }
                ),
                changed_tables_by_id={},
                created_tables_by_id={},
                destroyed_table_ids=["tbl20000000000000", "tbl20000000000001"],
                error=None,
                error_code=None,
                cursor=1
            )
        """
        if cursor < 1:
            raise ValueError("cursor must be non-zero")
        if limit is not None and limit < 1:
            raise ValueError("limit must be non-zero")

        url = f"{self._url}/payloads"
        options = {"cursor": cursor}
        count = 0
        for page in self._api.iterate_requests(
            method="GET",
            url=url,
            options=options,
            offset_field="cursor",
        ):
            payloads = page["payloads"]
            for index, payload in enumerate(payloads):
                payload = WebhookPayload.from_api(payload, self._api, context=self)
                payload.cursor = cursor + index
                yield payload
                count += 1
                if limit is not None and count >= limit:
                    return

            if not (payloads and page.get("mightHaveMore")):
                return
            cursor = page["cursor"]


class _NestedId(AirtableModel):
    id: str


class WebhookNotification(AirtableModel):
    """
    Represents the value that Airtable will POST to the webhook's notification URL.

    This will not contain the full webhook payload; it will only contain the IDs
    of the base and the webhook which triggered the notification. You will need to
    use :meth:`Webhook.payloads <pyairtable.models.Webhook.payloads>` to retrieve
    the actual payloads describing the change(s) which triggered the webhook.

    You will also need some way to persist the ``cursor`` of the webhook payload,
    so that on subsequent calls you do not retrieve the same payloads again.

    Usage:
        .. code-block:: python

            from flask import Flask, request
            from pyairtable import Api
            from pyairtable.models import WebhookNotification

            app = Flask(__name__)

            @app.route("/airtable-webhook", methods=["POST"])
            def airtable_webhook():
                body = request.data
                header = request.headers["X-Airtable-Content-MAC"]
                secret = app.config["AIRTABLE_WEBHOOK_SECRET"]
                event = WebhookNotification.from_request(body, header, secret)
                airtable = Api(app.config["AIRTABLE_API_KEY"])
                webhook = airtable.base(event.base.id).webhook(event.webhook.id)
                cursor = int(your_db.get(f"cursor_{event.webhook}", 0)) + 1
                for payload in webhook.payloads(cursor=cursor):
                    # ...do stuff...
                    your_db.set(f"cursor_{event.webhook}", payload.cursor)
                return ("", 204)  # intentionally empty response

    See `Webhook notification delivery <https://airtable.com/developers/web/api/webhooks-overview#webhook-notification-delivery>`_
    for more information on how these payloads are structured.
    """

    base: _NestedId
    webhook: _NestedId
    timestamp: str

    @classmethod
    def from_request(
        cls,
        body: str,
        header: str,
        secret: Union[bytes, str],
    ) -> SelfType:
        """
        Validate a request body and X-Airtable-Content-MAC header
        using the secret returned when the webhook was created.

        Args:
            body: The full request body sent over the wire.
            header: The request's X-Airtable-Content-MAC header.
            secret: The MAC secret provided when the webhook was created.
                If ``str``, it's assumed this is still base64-encoded;
                if ``bytes``, it's assumed that this has been decoded.

        Returns:
            :class:`~WebhookNotification`: An instance parsed from the provided request body.

        Raises:
            ValueError: if the header and body do not match the secret.
        """
        if isinstance(secret, str):
            secret = base64.decodebytes(secret.encode("ascii"))
        hmac = HMAC(secret, body.encode("ascii"), "sha256")
        expected = "hmac-sha256=" + hmac.hexdigest()
        if header != expected:
            raise ValueError("X-Airtable-Content-MAC header failed validation")
        return cls.parse_raw(body)


class WebhookNotificationResult(AirtableModel):
    success: bool
    completion_timestamp: str
    duration_ms: float
    retry_number: int
    will_be_retried: Optional[bool] = None
    error: Optional["WebhookError"] = None


class WebhookError(AirtableModel):
    message: str


class WebhookSpecification(AirtableModel):
    options: "WebhookSpecification.Options"

    class Options(AirtableModel):
        filters: "WebhookSpecification.Filters"
        includes: Optional["WebhookSpecification.Includes"]

    class Filters(AirtableModel):
        data_types: List[str]
        record_change_scope: Optional[str]
        change_types: List[str] = FL()
        from_sources: List[str] = FL()
        source_options: Optional["WebhookSpecification.SourceOptions"]
        watch_data_in_field_ids: List[str] = FL()
        watch_schemas_of_field_ids: List[str] = FL()

    class SourceOptions(AirtableModel):
        form_submission: Optional["WebhookSpecification.FormSubmission"]

    class FormSubmission(AirtableModel):
        view_id: str

    class Includes(AirtableModel):
        include_cell_values_in_field_ids: List[str] = FL()
        include_previous_cell_values: bool = False
        include_previous_field_definitions: bool = False


class CreateWebhook(AirtableModel):
    notification_url: Optional[str]
    specification: WebhookSpecification


class CreateWebhookResponse(AirtableModel):
    """
    Payload returned by :meth:`Base.add_webhook <pyairtable.Base.add_webhook>`
    which includes the base64-encoded MAC secret that you'll need in order to
    verify the authenticity of future webhook requests.
    """

    #: The ID of the webhook that was just created.
    id: str

    #: The base64-encoded MAC secret. This should be saved somewhere upon receipt;
    #: there is no way to retrieve it once this object is discarded.
    mac_secret_base64: str

    #: The timestamp when the webhook will expire and be deleted.
    expiration_time: Optional[str]


class WebhookPayload(AirtableModel):
    """
    Payload returned by :meth:`Webhook.payloads`. See API docs:
    `Webhooks payload <https://airtable.com/developers/web/api/model/webhooks-payload>`_.
    """

    timestamp: str
    base_transaction_number: int
    payload_format: str
    action_metadata: Optional["WebhookPayload.ActionMetadata"]
    changed_tables_by_id: Dict[str, "WebhookPayload.TableChanged"] = FD()
    created_tables_by_id: Dict[str, "WebhookPayload.TableCreated"] = FD()
    destroyed_table_ids: List[str] = FL()
    error: Optional[bool]
    error_code: Optional[str] = pydantic.Field(alias="code")

    #: This is not a part of Airtable's webhook payload specification.
    #: This indicates the cursor field in the response which provided this payload.
    cursor: Optional[int]

    class ActionMetadata(AirtableModel):
        source: str
        source_metadata: Dict[Any, Any] = FD()

    class TableInfo(AirtableModel):
        name: str = ""
        description: Optional[str] = None

    class FieldInfo(AirtableModel):
        name: Optional[str]
        type: Optional[str]

    class FieldChanged(AirtableModel):
        current: "WebhookPayload.FieldInfo"
        previous: Optional["WebhookPayload.FieldInfo"]

    class TableChanged(AirtableModel):
        changed_views_by_id: Dict[str, "WebhookPayload.ViewChanged"] = FD()
        changed_fields_by_id: Dict[str, "WebhookPayload.FieldChanged"] = FD()
        changed_records_by_id: Dict[RecordId, "WebhookPayload.RecordChanged"] = FD()
        created_fields_by_id: Dict[str, "WebhookPayload.FieldInfo"] = FD()
        created_records_by_id: Dict[RecordId, "WebhookPayload.RecordCreated"] = FD()
        changed_metadata: Optional["WebhookPayload.TableChanged.ChangedMetadata"]
        destroyed_field_ids: List[str] = FL()
        destroyed_record_ids: List[RecordId] = FL()

        class ChangedMetadata(AirtableModel):
            current: "WebhookPayload.TableInfo"
            previous: "WebhookPayload.TableInfo"

    class ViewChanged(AirtableModel):
        changed_records_by_id: Dict[RecordId, "WebhookPayload.RecordChanged"] = FD()
        created_records_by_id: Dict[RecordId, "WebhookPayload.RecordCreated"] = FD()
        destroyed_record_ids: List[RecordId] = FL()

    class TableCreated(AirtableModel):
        metadata: Optional["WebhookPayload.TableInfo"]
        fields_by_id: Dict[str, "WebhookPayload.FieldInfo"] = FD()
        records_by_id: Dict[RecordId, "WebhookPayload.RecordCreated"] = FD()

    class RecordChanged(AirtableModel):
        current: "WebhookPayload.CellValuesByFieldId"
        previous: Optional["WebhookPayload.CellValuesByFieldId"]
        unchanged: Optional["WebhookPayload.CellValuesByFieldId"]

    class CellValuesByFieldId(AirtableModel):
        cell_values_by_field_id: Dict[str, Any]

    class RecordCreated(AirtableModel):
        created_time: str
        cell_values_by_field_id: Dict[str, Any]


class WebhookPayloads(AirtableModel):
    cursor: int
    might_have_more: bool
    payloads: List[WebhookPayload]


update_forward_refs(vars())
