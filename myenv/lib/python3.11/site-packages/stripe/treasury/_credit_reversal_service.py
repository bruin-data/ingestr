# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.treasury._credit_reversal import CreditReversal
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class CreditReversalService(StripeService):
    class CreateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        received_credit: str
        """
        The ReceivedCredit to reverse.
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
        received_credit: NotRequired[str]
        """
        Only return CreditReversals for the ReceivedCredit ID.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        status: NotRequired[Literal["canceled", "posted", "processing"]]
        """
        Only return CreditReversals for a given status.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "CreditReversalService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[CreditReversal]:
        """
        Returns a list of CreditReversals.
        """
        return cast(
            ListObject[CreditReversal],
            self._request(
                "get",
                "/v1/treasury/credit_reversals",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "CreditReversalService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[CreditReversal]:
        """
        Returns a list of CreditReversals.
        """
        return cast(
            ListObject[CreditReversal],
            await self._request_async(
                "get",
                "/v1/treasury/credit_reversals",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "CreditReversalService.CreateParams",
        options: RequestOptions = {},
    ) -> CreditReversal:
        """
        Reverses a ReceivedCredit and creates a CreditReversal object.
        """
        return cast(
            CreditReversal,
            self._request(
                "post",
                "/v1/treasury/credit_reversals",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "CreditReversalService.CreateParams",
        options: RequestOptions = {},
    ) -> CreditReversal:
        """
        Reverses a ReceivedCredit and creates a CreditReversal object.
        """
        return cast(
            CreditReversal,
            await self._request_async(
                "post",
                "/v1/treasury/credit_reversals",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        credit_reversal: str,
        params: "CreditReversalService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> CreditReversal:
        """
        Retrieves the details of an existing CreditReversal by passing the unique CreditReversal ID from either the CreditReversal creation request or CreditReversal list
        """
        return cast(
            CreditReversal,
            self._request(
                "get",
                "/v1/treasury/credit_reversals/{credit_reversal}".format(
                    credit_reversal=sanitize_id(credit_reversal),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        credit_reversal: str,
        params: "CreditReversalService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> CreditReversal:
        """
        Retrieves the details of an existing CreditReversal by passing the unique CreditReversal ID from either the CreditReversal creation request or CreditReversal list
        """
        return cast(
            CreditReversal,
            await self._request_async(
                "get",
                "/v1/treasury/credit_reversals/{credit_reversal}".format(
                    credit_reversal=sanitize_id(credit_reversal),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
