# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._bank_account import BankAccount
from stripe._card import Card
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, Union, cast
from typing_extensions import Literal, NotRequired, TypedDict


class AccountExternalAccountService(StripeService):
    class CreateParams(TypedDict):
        default_for_currency: NotRequired[bool]
        """
        When set to true, or if this is the first external account added in this currency, this account becomes the default external account for its currency.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        external_account: Union[
            str,
            "AccountExternalAccountService.CreateParamsCard",
            "AccountExternalAccountService.CreateParamsBankAccount",
            "AccountExternalAccountService.CreateParamsCardToken",
        ]
        """
        Please refer to full [documentation](https://stripe.com/docs/api) instead.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class CreateParamsBankAccount(TypedDict):
        object: Literal["bank_account"]
        account_holder_name: NotRequired[str]
        """
        The name of the person or business that owns the bank account.This field is required when attaching the bank account to a `Customer` object.
        """
        account_holder_type: NotRequired[Literal["company", "individual"]]
        """
        The type of entity that holds the account. It can be `company` or `individual`. This field is required when attaching the bank account to a `Customer` object.
        """
        account_number: str
        """
        The account number for the bank account, in string form. Must be a checking account.
        """
        country: str
        """
        The country in which the bank account is located.
        """
        currency: NotRequired[str]
        """
        The currency the bank account is in. This must be a country/currency pairing that [Stripe supports.](docs/payouts)
        """
        routing_number: NotRequired[str]
        """
        The routing number, sort code, or other country-appropriateinstitution number for the bank account. For US bank accounts, this is required and should bethe ACH routing number, not the wire routing number. If you are providing an IBAN for`account_number`, this field is not required.
        """

    class CreateParamsCard(TypedDict):
        object: Literal["card"]
        address_city: NotRequired[str]
        address_country: NotRequired[str]
        address_line1: NotRequired[str]
        address_line2: NotRequired[str]
        address_state: NotRequired[str]
        address_zip: NotRequired[str]
        currency: NotRequired[str]
        cvc: NotRequired[str]
        exp_month: int
        exp_year: int
        name: NotRequired[str]
        number: str
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
        """

    class CreateParamsCardToken(TypedDict):
        object: Literal["card"]
        currency: NotRequired[str]
        token: str

    class DeleteParams(TypedDict):
        pass

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
        object: NotRequired[Literal["bank_account", "card"]]
        """
        Filter external accounts according to a particular object type.
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
        account_holder_type: NotRequired[
            "Literal['']|Literal['company', 'individual']"
        ]
        """
        The type of entity that holds the account. This can be either `individual` or `company`.
        """
        account_type: NotRequired[
            Literal["checking", "futsu", "savings", "toza"]
        ]
        """
        The bank account type. This can only be `checking` or `savings` in most countries. In Japan, this can only be `futsu` or `toza`.
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
        default_for_currency: NotRequired[bool]
        """
        When set to true, this becomes the default external account for its currency.
        """
        documents: NotRequired[
            "AccountExternalAccountService.UpdateParamsDocuments"
        ]
        """
        Documents that may be submitted to satisfy various informational requests.
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

    class UpdateParamsDocuments(TypedDict):
        bank_account_ownership_verification: NotRequired[
            "AccountExternalAccountService.UpdateParamsDocumentsBankAccountOwnershipVerification"
        ]
        """
        One or more documents that support the [Bank account ownership verification](https://support.stripe.com/questions/bank-account-ownership-verification) requirement. Must be a document associated with the bank account that displays the last 4 digits of the account number, either a statement or a voided check.
        """

    class UpdateParamsDocumentsBankAccountOwnershipVerification(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    def delete(
        self,
        account: str,
        id: str,
        params: "AccountExternalAccountService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> Union[BankAccount, Card]:
        """
        Delete a specified external account for a given account.
        """
        return cast(
            Union[BankAccount, Card],
            self._request(
                "delete",
                "/v1/accounts/{account}/external_accounts/{id}".format(
                    account=sanitize_id(account),
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
        account: str,
        id: str,
        params: "AccountExternalAccountService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> Union[BankAccount, Card]:
        """
        Delete a specified external account for a given account.
        """
        return cast(
            Union[BankAccount, Card],
            await self._request_async(
                "delete",
                "/v1/accounts/{account}/external_accounts/{id}".format(
                    account=sanitize_id(account),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        account: str,
        id: str,
        params: "AccountExternalAccountService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Union[BankAccount, Card]:
        """
        Retrieve a specified external account for a given account.
        """
        return cast(
            Union[BankAccount, Card],
            self._request(
                "get",
                "/v1/accounts/{account}/external_accounts/{id}".format(
                    account=sanitize_id(account),
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
        account: str,
        id: str,
        params: "AccountExternalAccountService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Union[BankAccount, Card]:
        """
        Retrieve a specified external account for a given account.
        """
        return cast(
            Union[BankAccount, Card],
            await self._request_async(
                "get",
                "/v1/accounts/{account}/external_accounts/{id}".format(
                    account=sanitize_id(account),
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
        account: str,
        id: str,
        params: "AccountExternalAccountService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Union[BankAccount, Card]:
        """
        Updates the metadata, account holder name, account holder type of a bank account belonging to
        a connected account and optionally sets it as the default for its currency. Other bank account
        details are not editable by design.

        You can only update bank accounts when [account.controller.requirement_collection is application, which includes <a href="/connect/custom-accounts">Custom accounts](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection).

        You can re-enable a disabled bank account by performing an update call without providing any
        arguments or changes.
        """
        return cast(
            Union[BankAccount, Card],
            self._request(
                "post",
                "/v1/accounts/{account}/external_accounts/{id}".format(
                    account=sanitize_id(account),
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
        account: str,
        id: str,
        params: "AccountExternalAccountService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Union[BankAccount, Card]:
        """
        Updates the metadata, account holder name, account holder type of a bank account belonging to
        a connected account and optionally sets it as the default for its currency. Other bank account
        details are not editable by design.

        You can only update bank accounts when [account.controller.requirement_collection is application, which includes <a href="/connect/custom-accounts">Custom accounts](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection).

        You can re-enable a disabled bank account by performing an update call without providing any
        arguments or changes.
        """
        return cast(
            Union[BankAccount, Card],
            await self._request_async(
                "post",
                "/v1/accounts/{account}/external_accounts/{id}".format(
                    account=sanitize_id(account),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def list(
        self,
        account: str,
        params: "AccountExternalAccountService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Union[BankAccount, Card]]:
        """
        List external accounts for an account.
        """
        return cast(
            ListObject[Union[BankAccount, Card]],
            self._request(
                "get",
                "/v1/accounts/{account}/external_accounts".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        account: str,
        params: "AccountExternalAccountService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Union[BankAccount, Card]]:
        """
        List external accounts for an account.
        """
        return cast(
            ListObject[Union[BankAccount, Card]],
            await self._request_async(
                "get",
                "/v1/accounts/{account}/external_accounts".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        account: str,
        params: "AccountExternalAccountService.CreateParams",
        options: RequestOptions = {},
    ) -> Union[BankAccount, Card]:
        """
        Create an external account for a given account.
        """
        return cast(
            Union[BankAccount, Card],
            self._request(
                "post",
                "/v1/accounts/{account}/external_accounts".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        account: str,
        params: "AccountExternalAccountService.CreateParams",
        options: RequestOptions = {},
    ) -> Union[BankAccount, Card]:
        """
        Create an external account for a given account.
        """
        return cast(
            Union[BankAccount, Card],
            await self._request_async(
                "post",
                "/v1/accounts/{account}/external_accounts".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
