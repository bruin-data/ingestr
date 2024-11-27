# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.treasury._outbound_payment import OutboundPayment
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class OutboundPaymentService(StripeService):
    class FailParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class PostParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ReturnOutboundPaymentParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        returned_details: NotRequired[
            "OutboundPaymentService.ReturnOutboundPaymentParamsReturnedDetails"
        ]
        """
        Optional hash to set the return code.
        """

    class ReturnOutboundPaymentParamsReturnedDetails(TypedDict):
        code: NotRequired[
            Literal[
                "account_closed",
                "account_frozen",
                "bank_account_restricted",
                "bank_ownership_changed",
                "declined",
                "incorrect_account_holder_name",
                "invalid_account_number",
                "invalid_currency",
                "no_account",
                "other",
            ]
        ]
        """
        The return code to be set on the OutboundPayment object.
        """

    class UpdateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        tracking_details: "OutboundPaymentService.UpdateParamsTrackingDetails"
        """
        Details about network-specific tracking information.
        """

    class UpdateParamsTrackingDetails(TypedDict):
        ach: NotRequired[
            "OutboundPaymentService.UpdateParamsTrackingDetailsAch"
        ]
        """
        ACH network tracking details.
        """
        type: Literal["ach", "us_domestic_wire"]
        """
        The US bank account network used to send funds.
        """
        us_domestic_wire: NotRequired[
            "OutboundPaymentService.UpdateParamsTrackingDetailsUsDomesticWire"
        ]
        """
        US domestic wire network tracking details.
        """

    class UpdateParamsTrackingDetailsAch(TypedDict):
        trace_id: str
        """
        ACH trace ID for funds sent over the `ach` network.
        """

    class UpdateParamsTrackingDetailsUsDomesticWire(TypedDict):
        imad: NotRequired[str]
        """
        IMAD for funds sent over the `us_domestic_wire` network.
        """
        omad: NotRequired[str]
        """
        OMAD for funds sent over the `us_domestic_wire` network.
        """

    def update(
        self,
        id: str,
        params: "OutboundPaymentService.UpdateParams",
        options: RequestOptions = {},
    ) -> OutboundPayment:
        """
        Updates a test mode created OutboundPayment with tracking details. The OutboundPayment must not be cancelable, and cannot be in the canceled or failed states.
        """
        return cast(
            OutboundPayment,
            self._request(
                "post",
                "/v1/test_helpers/treasury/outbound_payments/{id}".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        id: str,
        params: "OutboundPaymentService.UpdateParams",
        options: RequestOptions = {},
    ) -> OutboundPayment:
        """
        Updates a test mode created OutboundPayment with tracking details. The OutboundPayment must not be cancelable, and cannot be in the canceled or failed states.
        """
        return cast(
            OutboundPayment,
            await self._request_async(
                "post",
                "/v1/test_helpers/treasury/outbound_payments/{id}".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def fail(
        self,
        id: str,
        params: "OutboundPaymentService.FailParams" = {},
        options: RequestOptions = {},
    ) -> OutboundPayment:
        """
        Transitions a test mode created OutboundPayment to the failed status. The OutboundPayment must already be in the processing state.
        """
        return cast(
            OutboundPayment,
            self._request(
                "post",
                "/v1/test_helpers/treasury/outbound_payments/{id}/fail".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def fail_async(
        self,
        id: str,
        params: "OutboundPaymentService.FailParams" = {},
        options: RequestOptions = {},
    ) -> OutboundPayment:
        """
        Transitions a test mode created OutboundPayment to the failed status. The OutboundPayment must already be in the processing state.
        """
        return cast(
            OutboundPayment,
            await self._request_async(
                "post",
                "/v1/test_helpers/treasury/outbound_payments/{id}/fail".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def post(
        self,
        id: str,
        params: "OutboundPaymentService.PostParams" = {},
        options: RequestOptions = {},
    ) -> OutboundPayment:
        """
        Transitions a test mode created OutboundPayment to the posted status. The OutboundPayment must already be in the processing state.
        """
        return cast(
            OutboundPayment,
            self._request(
                "post",
                "/v1/test_helpers/treasury/outbound_payments/{id}/post".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def post_async(
        self,
        id: str,
        params: "OutboundPaymentService.PostParams" = {},
        options: RequestOptions = {},
    ) -> OutboundPayment:
        """
        Transitions a test mode created OutboundPayment to the posted status. The OutboundPayment must already be in the processing state.
        """
        return cast(
            OutboundPayment,
            await self._request_async(
                "post",
                "/v1/test_helpers/treasury/outbound_payments/{id}/post".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def return_outbound_payment(
        self,
        id: str,
        params: "OutboundPaymentService.ReturnOutboundPaymentParams" = {},
        options: RequestOptions = {},
    ) -> OutboundPayment:
        """
        Transitions a test mode created OutboundPayment to the returned status. The OutboundPayment must already be in the processing state.
        """
        return cast(
            OutboundPayment,
            self._request(
                "post",
                "/v1/test_helpers/treasury/outbound_payments/{id}/return".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def return_outbound_payment_async(
        self,
        id: str,
        params: "OutboundPaymentService.ReturnOutboundPaymentParams" = {},
        options: RequestOptions = {},
    ) -> OutboundPayment:
        """
        Transitions a test mode created OutboundPayment to the returned status. The OutboundPayment must already be in the processing state.
        """
        return cast(
            OutboundPayment,
            await self._request_async(
                "post",
                "/v1/test_helpers/treasury/outbound_payments/{id}/return".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
