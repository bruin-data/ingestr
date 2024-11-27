# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._tax_code import TaxCode
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class TaxCodeService(StripeService):
    class ListParams(TypedDict):
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
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "TaxCodeService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[TaxCode]:
        """
        A list of [all tax codes available](https://stripe.com/docs/tax/tax-categories) to add to Products in order to allow specific tax calculations.
        """
        return cast(
            ListObject[TaxCode],
            self._request(
                "get",
                "/v1/tax_codes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "TaxCodeService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[TaxCode]:
        """
        A list of [all tax codes available](https://stripe.com/docs/tax/tax-categories) to add to Products in order to allow specific tax calculations.
        """
        return cast(
            ListObject[TaxCode],
            await self._request_async(
                "get",
                "/v1/tax_codes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        id: str,
        params: "TaxCodeService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> TaxCode:
        """
        Retrieves the details of an existing tax code. Supply the unique tax code ID and Stripe will return the corresponding tax code information.
        """
        return cast(
            TaxCode,
            self._request(
                "get",
                "/v1/tax_codes/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        id: str,
        params: "TaxCodeService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> TaxCode:
        """
        Retrieves the details of an existing tax code. Supply the unique tax code ID and Stripe will return the corresponding tax code information.
        """
        return cast(
            TaxCode,
            await self._request_async(
                "get",
                "/v1/tax_codes/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
