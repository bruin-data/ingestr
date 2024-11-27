# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._account import Account
from stripe._bank_account import BankAccount
from stripe._card import Card
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._source import Source
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, Union, cast
from typing_extensions import Literal, NotRequired, TypedDict


class CustomerPaymentSourceService(StripeService):
    class CreateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        source: str
        """
        Please refer to full [documentation](https://stripe.com/docs/api) instead.
        """
        validate: NotRequired[bool]

    class DeleteParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
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
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and 100, and the default is 10.
        """
        object: NotRequired[str]
        """
        Filter sources according to a particular object type.
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

    class UpdateParams(TypedDict):
        account_holder_name: NotRequired[str]
        """
        The name of the person or business that owns the bank account.
        """
        account_holder_type: NotRequired[Literal["company", "individual"]]
        """
        The type of entity that holds the account. This can be either `individual` or `company`.
        """
        address_city: NotRequired[str]
        """
        City/District/Suburb/Town/Village.
        """
        address_country: NotRequired[str]
        """
        Billing address country, if provided when creating card.
        """
        address_line1: NotRequired[str]
        """
        Address line 1 (Street address/PO Box/Company name).
        """
        address_line2: NotRequired[str]
        """
        Address line 2 (Apartment/Suite/Unit/Building).
        """
        address_state: NotRequired[str]
        """
        State/County/Province/Region.
        """
        address_zip: NotRequired[str]
        """
        ZIP or postal code.
        """
        exp_month: NotRequired[str]
        """
        Two digit number representing the card's expiration month.
        """
        exp_year: NotRequired[str]
        """
        Four digit number representing the card's expiration year.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        name: NotRequired[str]
        """
        Cardholder name.
        """
        owner: NotRequired["CustomerPaymentSourceService.UpdateParamsOwner"]

    class UpdateParamsOwner(TypedDict):
        address: NotRequired[
            "CustomerPaymentSourceService.UpdateParamsOwnerAddress"
        ]
        """
        Owner's address.
        """
        email: NotRequired[str]
        """
        Owner's email address.
        """
        name: NotRequired[str]
        """
        Owner's full name.
        """
        phone: NotRequired[str]
        """
        Owner's phone number.
        """

    class UpdateParamsOwnerAddress(TypedDict):
        city: NotRequired[str]
        """
        City, district, suburb, town, or village.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Address line 1 (e.g., street, PO Box, or company name).
        """
        line2: NotRequired[str]
        """
        Address line 2 (e.g., apartment, suite, unit, or building).
        """
        postal_code: NotRequired[str]
        """
        ZIP or postal code.
        """
        state: NotRequired[str]
        """
        State, county, province, or region.
        """

    class VerifyParams(TypedDict):
        amounts: NotRequired[List[int]]
        """
        Two positive integers, in *cents*, equal to the values of the microdeposits sent to the bank account.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        customer: str,
        params: "CustomerPaymentSourceService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Union[Account, BankAccount, Card, Source]]:
        """
        List sources for a specified customer.
        """
        return cast(
            ListObject[Union[Account, BankAccount, Card, Source]],
            self._request(
                "get",
                "/v1/customers/{customer}/sources".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        customer: str,
        params: "CustomerPaymentSourceService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Union[Account, BankAccount, Card, Source]]:
        """
        List sources for a specified customer.
        """
        return cast(
            ListObject[Union[Account, BankAccount, Card, Source]],
            await self._request_async(
                "get",
                "/v1/customers/{customer}/sources".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        customer: str,
        params: "CustomerPaymentSourceService.CreateParams",
        options: RequestOptions = {},
    ) -> Union[Account, BankAccount, Card, Source]:
        """
        When you create a new credit card, you must specify a customer or recipient on which to create it.

        If the card's owner has no default card, then the new card will become the default.
        However, if the owner already has a default, then it will not change.
        To change the default, you should [update the customer](https://stripe.com/docs/api#update_customer) to have a new default_source.
        """
        return cast(
            Union[Account, BankAccount, Card, Source],
            self._request(
                "post",
                "/v1/customers/{customer}/sources".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        customer: str,
        params: "CustomerPaymentSourceService.CreateParams",
        options: RequestOptions = {},
    ) -> Union[Account, BankAccount, Card, Source]:
        """
        When you create a new credit card, you must specify a customer or recipient on which to create it.

        If the card's owner has no default card, then the new card will become the default.
        However, if the owner already has a default, then it will not change.
        To change the default, you should [update the customer](https://stripe.com/docs/api#update_customer) to have a new default_source.
        """
        return cast(
            Union[Account, BankAccount, Card, Source],
            await self._request_async(
                "post",
                "/v1/customers/{customer}/sources".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        customer: str,
        id: str,
        params: "CustomerPaymentSourceService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Union[Account, BankAccount, Card, Source]:
        """
        Retrieve a specified source for a given customer.
        """
        return cast(
            Union[Account, BankAccount, Card, Source],
            self._request(
                "get",
                "/v1/customers/{customer}/sources/{id}".format(
                    customer=sanitize_id(customer),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        customer: str,
        id: str,
        params: "CustomerPaymentSourceService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Union[Account, BankAccount, Card, Source]:
        """
        Retrieve a specified source for a given customer.
        """
        return cast(
            Union[Account, BankAccount, Card, Source],
            await self._request_async(
                "get",
                "/v1/customers/{customer}/sources/{id}".format(
                    customer=sanitize_id(customer),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        customer: str,
        id: str,
        params: "CustomerPaymentSourceService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Union[Account, BankAccount, Card, Source]:
        """
        Update a specified source for a given customer.
        """
        return cast(
            Union[Account, BankAccount, Card, Source],
            self._request(
                "post",
                "/v1/customers/{customer}/sources/{id}".format(
                    customer=sanitize_id(customer),
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
        customer: str,
        id: str,
        params: "CustomerPaymentSourceService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Union[Account, BankAccount, Card, Source]:
        """
        Update a specified source for a given customer.
        """
        return cast(
            Union[Account, BankAccount, Card, Source],
            await self._request_async(
                "post",
                "/v1/customers/{customer}/sources/{id}".format(
                    customer=sanitize_id(customer),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def delete(
        self,
        customer: str,
        id: str,
        params: "CustomerPaymentSourceService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> Union[Account, BankAccount, Card, Source]:
        """
        Delete a specified source for a given customer.
        """
        return cast(
            Union[Account, BankAccount, Card, Source],
            self._request(
                "delete",
                "/v1/customers/{customer}/sources/{id}".format(
                    customer=sanitize_id(customer),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def delete_async(
        self,
        customer: str,
        id: str,
        params: "CustomerPaymentSourceService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> Union[Account, BankAccount, Card, Source]:
        """
        Delete a specified source for a given customer.
        """
        return cast(
            Union[Account, BankAccount, Card, Source],
            await self._request_async(
                "delete",
                "/v1/customers/{customer}/sources/{id}".format(
                    customer=sanitize_id(customer),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def verify(
        self,
        customer: str,
        id: str,
        params: "CustomerPaymentSourceService.VerifyParams" = {},
        options: RequestOptions = {},
    ) -> BankAccount:
        """
        Verify a specified bank account for a given customer.
        """
        return cast(
            BankAccount,
            self._request(
                "post",
                "/v1/customers/{customer}/sources/{id}/verify".format(
                    customer=sanitize_id(customer),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def verify_async(
        self,
        customer: str,
        id: str,
        params: "CustomerPaymentSourceService.VerifyParams" = {},
        options: RequestOptions = {},
    ) -> BankAccount:
        """
        Verify a specified bank account for a given customer.
        """
        return cast(
            BankAccount,
            await self._request_async(
                "post",
                "/v1/customers/{customer}/sources/{id}/verify".format(
                    customer=sanitize_id(customer),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
