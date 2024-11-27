# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.identity._verification_session import VerificationSession
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class VerificationSessionService(StripeService):
    class CancelParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CreateParams(TypedDict):
        client_reference_id: NotRequired[str]
        """
        A string to reference this user. This can be a customer ID, a session ID, or similar, and can be used to reconcile this verification with your internal systems.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        options: NotRequired["VerificationSessionService.CreateParamsOptions"]
        """
        A set of options for the session's verification checks.
        """
        provided_details: NotRequired[
            "VerificationSessionService.CreateParamsProvidedDetails"
        ]
        """
        Details provided about the user being verified. These details may be shown to the user.
        """
        related_customer: NotRequired[str]
        """
        Token referencing a Customer resource.
        """
        return_url: NotRequired[str]
        """
        The URL that the user will be redirected to upon completing the verification flow.
        """
        type: NotRequired[Literal["document", "id_number"]]
        """
        The type of [verification check](https://stripe.com/docs/identity/verification-checks) to be performed. You must provide a `type` if not passing `verification_flow`.
        """
        verification_flow: NotRequired[str]
        """
        The ID of a Verification Flow from the Dashboard. See https://docs.stripe.com/identity/verification-flows.
        """

    class CreateParamsOptions(TypedDict):
        document: NotRequired[
            "Literal['']|VerificationSessionService.CreateParamsOptionsDocument"
        ]
        """
        Options that apply to the [document check](https://stripe.com/docs/identity/verification-checks?type=document).
        """

    class CreateParamsOptionsDocument(TypedDict):
        allowed_types: NotRequired[
            List[Literal["driving_license", "id_card", "passport"]]
        ]
        """
        Array of strings of allowed identity document types. If the provided identity document isn't one of the allowed types, the verification check will fail with a document_type_not_allowed error code.
        """
        require_id_number: NotRequired[bool]
        """
        Collect an ID number and perform an [ID number check](https://stripe.com/docs/identity/verification-checks?type=id-number) with the document's extracted name and date of birth.
        """
        require_live_capture: NotRequired[bool]
        """
        Disable image uploads, identity document images have to be captured using the device's camera.
        """
        require_matching_selfie: NotRequired[bool]
        """
        Capture a face image and perform a [selfie check](https://stripe.com/docs/identity/verification-checks?type=selfie) comparing a photo ID and a picture of your user's face. [Learn more](https://stripe.com/docs/identity/selfie).
        """

    class CreateParamsProvidedDetails(TypedDict):
        email: NotRequired[str]
        """
        Email of user being verified
        """
        phone: NotRequired[str]
        """
        Phone number of user being verified
        """

    class ListParams(TypedDict):
        client_reference_id: NotRequired[str]
        """
        A string to reference this user. This can be a customer ID, a session ID, or similar, and can be used to reconcile this verification with your internal systems.
        """
        created: NotRequired[
            "VerificationSessionService.ListParamsCreated|int"
        ]
        """
        Only return VerificationSessions that were created during the given date interval.
        """
        ending_before: NotRequired[str]
        """
        A cursor for use in pagination. `ending_before` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, starting with `obj_bar`, your subsequent call can include `ending_before=obj_bar` in order to fetch the previous page of the list.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and 100, and the default is 10.
        """
        related_customer: NotRequired[str]
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        status: NotRequired[
            Literal["canceled", "processing", "requires_input", "verified"]
        ]
        """
        Only return VerificationSessions with this status. [Learn more about the lifecycle of sessions](https://stripe.com/docs/identity/how-sessions-work).
        """

    class ListParamsCreated(TypedDict):
        gt: NotRequired[int]
        """
        Minimum value to filter by (exclusive)
        """
        gte: NotRequired[int]
        """
        Minimum value to filter by (inclusive)
        """
        lt: NotRequired[int]
        """
        Maximum value to filter by (exclusive)
        """
        lte: NotRequired[int]
        """
        Maximum value to filter by (inclusive)
        """

    class RedactParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        options: NotRequired["VerificationSessionService.UpdateParamsOptions"]
        """
        A set of options for the session's verification checks.
        """
        provided_details: NotRequired[
            "VerificationSessionService.UpdateParamsProvidedDetails"
        ]
        """
        Details provided about the user being verified. These details may be shown to the user.
        """
        type: NotRequired[Literal["document", "id_number"]]
        """
        The type of [verification check](https://stripe.com/docs/identity/verification-checks) to be performed.
        """

    class UpdateParamsOptions(TypedDict):
        document: NotRequired[
            "Literal['']|VerificationSessionService.UpdateParamsOptionsDocument"
        ]
        """
        Options that apply to the [document check](https://stripe.com/docs/identity/verification-checks?type=document).
        """

    class UpdateParamsOptionsDocument(TypedDict):
        allowed_types: NotRequired[
            List[Literal["driving_license", "id_card", "passport"]]
        ]
        """
        Array of strings of allowed identity document types. If the provided identity document isn't one of the allowed types, the verification check will fail with a document_type_not_allowed error code.
        """
        require_id_number: NotRequired[bool]
        """
        Collect an ID number and perform an [ID number check](https://stripe.com/docs/identity/verification-checks?type=id-number) with the document's extracted name and date of birth.
        """
        require_live_capture: NotRequired[bool]
        """
        Disable image uploads, identity document images have to be captured using the device's camera.
        """
        require_matching_selfie: NotRequired[bool]
        """
        Capture a face image and perform a [selfie check](https://stripe.com/docs/identity/verification-checks?type=selfie) comparing a photo ID and a picture of your user's face. [Learn more](https://stripe.com/docs/identity/selfie).
        """

    class UpdateParamsProvidedDetails(TypedDict):
        email: NotRequired[str]
        """
        Email of user being verified
        """
        phone: NotRequired[str]
        """
        Phone number of user being verified
        """

    def list(
        self,
        params: "VerificationSessionService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[VerificationSession]:
        """
        Returns a list of VerificationSessions
        """
        return cast(
            ListObject[VerificationSession],
            self._request(
                "get",
                "/v1/identity/verification_sessions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "VerificationSessionService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[VerificationSession]:
        """
        Returns a list of VerificationSessions
        """
        return cast(
            ListObject[VerificationSession],
            await self._request_async(
                "get",
                "/v1/identity/verification_sessions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "VerificationSessionService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> VerificationSession:
        """
        Creates a VerificationSession object.

        After the VerificationSession is created, display a verification modal using the session client_secret or send your users to the session's url.

        If your API key is in test mode, verification checks won't actually process, though everything else will occur as if in live mode.

        Related guide: [Verify your users' identity documents](https://stripe.com/docs/identity/verify-identity-documents)
        """
        return cast(
            VerificationSession,
            self._request(
                "post",
                "/v1/identity/verification_sessions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "VerificationSessionService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> VerificationSession:
        """
        Creates a VerificationSession object.

        After the VerificationSession is created, display a verification modal using the session client_secret or send your users to the session's url.

        If your API key is in test mode, verification checks won't actually process, though everything else will occur as if in live mode.

        Related guide: [Verify your users' identity documents](https://stripe.com/docs/identity/verify-identity-documents)
        """
        return cast(
            VerificationSession,
            await self._request_async(
                "post",
                "/v1/identity/verification_sessions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        session: str,
        params: "VerificationSessionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> VerificationSession:
        """
        Retrieves the details of a VerificationSession that was previously created.

        When the session status is requires_input, you can use this method to retrieve a valid
        client_secret or url to allow re-submission.
        """
        return cast(
            VerificationSession,
            self._request(
                "get",
                "/v1/identity/verification_sessions/{session}".format(
                    session=sanitize_id(session),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        session: str,
        params: "VerificationSessionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> VerificationSession:
        """
        Retrieves the details of a VerificationSession that was previously created.

        When the session status is requires_input, you can use this method to retrieve a valid
        client_secret or url to allow re-submission.
        """
        return cast(
            VerificationSession,
            await self._request_async(
                "get",
                "/v1/identity/verification_sessions/{session}".format(
                    session=sanitize_id(session),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        session: str,
        params: "VerificationSessionService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> VerificationSession:
        """
        Updates a VerificationSession object.

        When the session status is requires_input, you can use this method to update the
        verification check and options.
        """
        return cast(
            VerificationSession,
            self._request(
                "post",
                "/v1/identity/verification_sessions/{session}".format(
                    session=sanitize_id(session),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        session: str,
        params: "VerificationSessionService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> VerificationSession:
        """
        Updates a VerificationSession object.

        When the session status is requires_input, you can use this method to update the
        verification check and options.
        """
        return cast(
            VerificationSession,
            await self._request_async(
                "post",
                "/v1/identity/verification_sessions/{session}".format(
                    session=sanitize_id(session),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def cancel(
        self,
        session: str,
        params: "VerificationSessionService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> VerificationSession:
        """
        A VerificationSession object can be canceled when it is in requires_input [status](https://stripe.com/docs/identity/how-sessions-work).

        Once canceled, future submission attempts are disabled. This cannot be undone. [Learn more](https://stripe.com/docs/identity/verification-sessions#cancel).
        """
        return cast(
            VerificationSession,
            self._request(
                "post",
                "/v1/identity/verification_sessions/{session}/cancel".format(
                    session=sanitize_id(session),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def cancel_async(
        self,
        session: str,
        params: "VerificationSessionService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> VerificationSession:
        """
        A VerificationSession object can be canceled when it is in requires_input [status](https://stripe.com/docs/identity/how-sessions-work).

        Once canceled, future submission attempts are disabled. This cannot be undone. [Learn more](https://stripe.com/docs/identity/verification-sessions#cancel).
        """
        return cast(
            VerificationSession,
            await self._request_async(
                "post",
                "/v1/identity/verification_sessions/{session}/cancel".format(
                    session=sanitize_id(session),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def redact(
        self,
        session: str,
        params: "VerificationSessionService.RedactParams" = {},
        options: RequestOptions = {},
    ) -> VerificationSession:
        """
        Redact a VerificationSession to remove all collected information from Stripe. This will redact
        the VerificationSession and all objects related to it, including VerificationReports, Events,
        request logs, etc.

        A VerificationSession object can be redacted when it is in requires_input or verified
        [status](https://stripe.com/docs/identity/how-sessions-work). Redacting a VerificationSession in requires_action
        state will automatically cancel it.

        The redaction process may take up to four days. When the redaction process is in progress, the
        VerificationSession's redaction.status field will be set to processing; when the process is
        finished, it will change to redacted and an identity.verification_session.redacted event
        will be emitted.

        Redaction is irreversible. Redacted objects are still accessible in the Stripe API, but all the
        fields that contain personal data will be replaced by the string [redacted] or a similar
        placeholder. The metadata field will also be erased. Redacted objects cannot be updated or
        used for any purpose.

        [Learn more](https://stripe.com/docs/identity/verification-sessions#redact).
        """
        return cast(
            VerificationSession,
            self._request(
                "post",
                "/v1/identity/verification_sessions/{session}/redact".format(
                    session=sanitize_id(session),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def redact_async(
        self,
        session: str,
        params: "VerificationSessionService.RedactParams" = {},
        options: RequestOptions = {},
    ) -> VerificationSession:
        """
        Redact a VerificationSession to remove all collected information from Stripe. This will redact
        the VerificationSession and all objects related to it, including VerificationReports, Events,
        request logs, etc.

        A VerificationSession object can be redacted when it is in requires_input or verified
        [status](https://stripe.com/docs/identity/how-sessions-work). Redacting a VerificationSession in requires_action
        state will automatically cancel it.

        The redaction process may take up to four days. When the redaction process is in progress, the
        VerificationSession's redaction.status field will be set to processing; when the process is
        finished, it will change to redacted and an identity.verification_session.redacted event
        will be emitted.

        Redaction is irreversible. Redacted objects are still accessible in the Stripe API, but all the
        fields that contain personal data will be replaced by the string [redacted] or a similar
        placeholder. The metadata field will also be erased. Redacted objects cannot be updated or
        used for any purpose.

        [Learn more](https://stripe.com/docs/identity/verification-sessions#redact).
        """
        return cast(
            VerificationSession,
            await self._request_async(
                "post",
                "/v1/identity/verification_sessions/{session}/redact".format(
                    session=sanitize_id(session),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
