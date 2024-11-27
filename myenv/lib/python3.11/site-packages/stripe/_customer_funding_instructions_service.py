# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._funding_instructions import FundingInstructions
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class CustomerFundingInstructionsService(StripeService):
    class CreateParams(TypedDict):
        bank_transfer: (
            "CustomerFundingInstructionsService.CreateParamsBankTransfer"
        )
        """
        Additional parameters for `bank_transfer` funding types
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        funding_type: Literal["bank_transfer"]
        """
        The `funding_type` to get the instructions for.
        """

    class CreateParamsBankTransfer(TypedDict):
        eu_bank_transfer: NotRequired[
            "CustomerFundingInstructionsService.CreateParamsBankTransferEuBankTransfer"
        ]
        """
        Configuration for eu_bank_transfer funding type.
        """
        requested_address_types: NotRequired[
            List[Literal["iban", "sort_code", "spei", "zengin"]]
        ]
        """
        List of address types that should be returned in the financial_addresses response. If not specified, all valid types will be returned.

        Permitted values include: `sort_code`, `zengin`, `iban`, or `spei`.
        """
        type: Literal[
            "eu_bank_transfer",
            "gb_bank_transfer",
            "jp_bank_transfer",
            "mx_bank_transfer",
            "us_bank_transfer",
        ]
        """
        The type of the `bank_transfer`
        """

    class CreateParamsBankTransferEuBankTransfer(TypedDict):
        country: str
        """
        The desired country code of the bank account information. Permitted values include: `BE`, `DE`, `ES`, `FR`, `IE`, or `NL`.
        """

    def create(
        self,
        customer: str,
        params: "CustomerFundingInstructionsService.CreateParams",
        options: RequestOptions = {},
    ) -> FundingInstructions:
        """
        Retrieve funding instructions for a customer cash balance. If funding instructions do not yet exist for the customer, new
        funding instructions will be created. If funding instructions have already been created for a given customer, the same
        funding instructions will be retrieved. In other words, we will return the same funding instructions each time.
        """
        return cast(
            FundingInstructions,
            self._request(
                "post",
                "/v1/customers/{customer}/funding_instructions".format(
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
        params: "CustomerFundingInstructionsService.CreateParams",
        options: RequestOptions = {},
    ) -> FundingInstructions:
        """
        Retrieve funding instructions for a customer cash balance. If funding instructions do not yet exist for the customer, new
        funding instructions will be created. If funding instructions have already been created for a given customer, the same
        funding instructions will be retrieved. In other words, we will return the same funding instructions each time.
        """
        return cast(
            FundingInstructions,
            await self._request_async(
                "post",
                "/v1/customers/{customer}/funding_instructions".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
