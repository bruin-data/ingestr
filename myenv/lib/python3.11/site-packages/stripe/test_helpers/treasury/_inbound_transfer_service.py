# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.treasury._inbound_transfer import InboundTransfer
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class InboundTransferService(StripeService):
    class FailParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        failure_details: NotRequired[
            "InboundTransferService.FailParamsFailureDetails"
        ]
        """
        Details about a failed InboundTransfer.
        """

    class FailParamsFailureDetails(TypedDict):
        code: NotRequired[
            Literal[
                "account_closed",
                "account_frozen",
                "bank_account_restricted",
                "bank_ownership_changed",
                "debit_not_authorized",
                "incorrect_account_holder_address",
                "incorrect_account_holder_name",
                "incorrect_account_holder_tax_id",
                "insufficient_funds",
                "invalid_account_number",
                "invalid_currency",
                "no_account",
                "other",
            ]
        ]
        """
        Reason for the failure.
        """

    class ReturnInboundTransferParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class SucceedParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def fail(
        self,
        id: str,
        params: "InboundTransferService.FailParams" = {},
        options: RequestOptions = {},
    ) -> InboundTransfer:
        """
        Transitions a test mode created InboundTransfer to the failed status. The InboundTransfer must already be in the processing state.
        """
        return cast(
            InboundTransfer,
            self._request(
                "post",
                "/v1/test_helpers/treasury/inbound_transfers/{id}/fail".format(
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
        params: "InboundTransferService.FailParams" = {},
        options: RequestOptions = {},
    ) -> InboundTransfer:
        """
        Transitions a test mode created InboundTransfer to the failed status. The InboundTransfer must already be in the processing state.
        """
        return cast(
            InboundTransfer,
            await self._request_async(
                "post",
                "/v1/test_helpers/treasury/inbound_transfers/{id}/fail".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def return_inbound_transfer(
        self,
        id: str,
        params: "InboundTransferService.ReturnInboundTransferParams" = {},
        options: RequestOptions = {},
    ) -> InboundTransfer:
        """
        Marks the test mode InboundTransfer object as returned and links the InboundTransfer to a ReceivedDebit. The InboundTransfer must already be in the succeeded state.
        """
        return cast(
            InboundTransfer,
            self._request(
                "post",
                "/v1/test_helpers/treasury/inbound_transfers/{id}/return".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def return_inbound_transfer_async(
        self,
        id: str,
        params: "InboundTransferService.ReturnInboundTransferParams" = {},
        options: RequestOptions = {},
    ) -> InboundTransfer:
        """
        Marks the test mode InboundTransfer object as returned and links the InboundTransfer to a ReceivedDebit. The InboundTransfer must already be in the succeeded state.
        """
        return cast(
            InboundTransfer,
            await self._request_async(
                "post",
                "/v1/test_helpers/treasury/inbound_transfers/{id}/return".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def succeed(
        self,
        id: str,
        params: "InboundTransferService.SucceedParams" = {},
        options: RequestOptions = {},
    ) -> InboundTransfer:
        """
        Transitions a test mode created InboundTransfer to the succeeded status. The InboundTransfer must already be in the processing state.
        """
        return cast(
            InboundTransfer,
            self._request(
                "post",
                "/v1/test_helpers/treasury/inbound_transfers/{id}/succeed".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def succeed_async(
        self,
        id: str,
        params: "InboundTransferService.SucceedParams" = {},
        options: RequestOptions = {},
    ) -> InboundTransfer:
        """
        Transitions a test mode created InboundTransfer to the succeeded status. The InboundTransfer must already be in the processing state.
        """
        return cast(
            InboundTransfer,
            await self._request_async(
                "post",
                "/v1/test_helpers/treasury/inbound_transfers/{id}/succeed".format(
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
