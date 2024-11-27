# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.treasury._debit_reversal import DebitReversal
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class DebitReversalService(StripeService):
    class CreateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        received_debit: str
        """
        The ReceivedDebit to reverse.
        """

    class ListParams(TypedDict):
        ending_before: NotRequired[str]
        """
        A cursor for use in pagination. `ending_before` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, starting with `obj_bar`, your subsequent call can include `ending_before=obj_bar` in order to fetch the previous page of the list.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        financial_account: str
        """
        Returns objects associated with this FinancialAccount.
        """
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and 100, and the default is 10.
        """
        received_debit: NotRequired[str]
        """
        Only return DebitReversals for the ReceivedDebit ID.
        """
        resolution: NotRequired[Literal["lost", "won"]]
        """
        Only return DebitReversals for a given resolution.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        status: NotRequired[Literal["canceled", "completed", "processing"]]
        """
        Only return DebitReversals for a given status.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "DebitReversalService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[DebitReversal]:
        """
        Returns a list of DebitReversals.
        """
        return cast(
            ListObject[DebitReversal],
            self._request(
                "get",
                "/v1/treasury/debit_reversals",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "DebitReversalService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[DebitReversal]:
        """
        Returns a list of DebitReversals.
        """
        return cast(
            ListObject[DebitReversal],
            await self._request_async(
                "get",
                "/v1/treasury/debit_reversals",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "DebitReversalService.CreateParams",
        options: RequestOptions = {},
    ) -> DebitReversal:
        """
        Reverses a ReceivedDebit and creates a DebitReversal object.
        """
        return cast(
            DebitReversal,
            self._request(
                "post",
                "/v1/treasury/debit_reversals",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "DebitReversalService.CreateParams",
        options: RequestOptions = {},
    ) -> DebitReversal:
        """
        Reverses a ReceivedDebit and creates a DebitReversal object.
        """
        return cast(
            DebitReversal,
            await self._request_async(
                "post",
                "/v1/treasury/debit_reversals",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        debit_reversal: str,
        params: "DebitReversalService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> DebitReversal:
        """
        Retrieves a DebitReversal object.
        """
        return cast(
            DebitReversal,
            self._request(
                "get",
                "/v1/treasury/debit_reversals/{debit_reversal}".format(
                    debit_reversal=sanitize_id(debit_reversal),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        debit_reversal: str,
        params: "DebitReversalService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> DebitReversal:
        """
        Retrieves a DebitReversal object.
        """
        return cast(
            DebitReversal,
            await self._request_async(
                "get",
                "/v1/treasury/debit_reversals/{debit_reversal}".format(
                    debit_reversal=sanitize_id(debit_reversal),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
