# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.treasury._financial_account_features import (
    FinancialAccountFeatures,
)
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class FinancialAccountFeaturesService(StripeService):
    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        card_issuing: NotRequired[
            "FinancialAccountFeaturesService.UpdateParamsCardIssuing"
        ]
        """
        Encodes the FinancialAccount's ability to be used with the Issuing product, including attaching cards to and drawing funds from the FinancialAccount.
        """
        deposit_insurance: NotRequired[
            "FinancialAccountFeaturesService.UpdateParamsDepositInsurance"
        ]
        """
        Represents whether this FinancialAccount is eligible for deposit insurance. Various factors determine the insurance amount.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        financial_addresses: NotRequired[
            "FinancialAccountFeaturesService.UpdateParamsFinancialAddresses"
        ]
        """
        Contains Features that add FinancialAddresses to the FinancialAccount.
        """
        inbound_transfers: NotRequired[
            "FinancialAccountFeaturesService.UpdateParamsInboundTransfers"
        ]
        """
        Contains settings related to adding funds to a FinancialAccount from another Account with the same owner.
        """
        intra_stripe_flows: NotRequired[
            "FinancialAccountFeaturesService.UpdateParamsIntraStripeFlows"
        ]
        """
        Represents the ability for the FinancialAccount to send money to, or receive money from other FinancialAccounts (for example, via OutboundPayment).
        """
        outbound_payments: NotRequired[
            "FinancialAccountFeaturesService.UpdateParamsOutboundPayments"
        ]
        """
        Includes Features related to initiating money movement out of the FinancialAccount to someone else's bucket of money.
        """
        outbound_transfers: NotRequired[
            "FinancialAccountFeaturesService.UpdateParamsOutboundTransfers"
        ]
        """
        Contains a Feature and settings related to moving money out of the FinancialAccount into another Account with the same owner.
        """

    class UpdateParamsCardIssuing(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class UpdateParamsDepositInsurance(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class UpdateParamsFinancialAddresses(TypedDict):
        aba: NotRequired[
            "FinancialAccountFeaturesService.UpdateParamsFinancialAddressesAba"
        ]
        """
        Adds an ABA FinancialAddress to the FinancialAccount.
        """

    class UpdateParamsFinancialAddressesAba(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class UpdateParamsInboundTransfers(TypedDict):
        ach: NotRequired[
            "FinancialAccountFeaturesService.UpdateParamsInboundTransfersAch"
        ]
        """
        Enables ACH Debits via the InboundTransfers API.
        """

    class UpdateParamsInboundTransfersAch(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class UpdateParamsIntraStripeFlows(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class UpdateParamsOutboundPayments(TypedDict):
        ach: NotRequired[
            "FinancialAccountFeaturesService.UpdateParamsOutboundPaymentsAch"
        ]
        """
        Enables ACH transfers via the OutboundPayments API.
        """
        us_domestic_wire: NotRequired[
            "FinancialAccountFeaturesService.UpdateParamsOutboundPaymentsUsDomesticWire"
        ]
        """
        Enables US domestic wire transfers via the OutboundPayments API.
        """

    class UpdateParamsOutboundPaymentsAch(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class UpdateParamsOutboundPaymentsUsDomesticWire(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class UpdateParamsOutboundTransfers(TypedDict):
        ach: NotRequired[
            "FinancialAccountFeaturesService.UpdateParamsOutboundTransfersAch"
        ]
        """
        Enables ACH transfers via the OutboundTransfers API.
        """
        us_domestic_wire: NotRequired[
            "FinancialAccountFeaturesService.UpdateParamsOutboundTransfersUsDomesticWire"
        ]
        """
        Enables US domestic wire transfers via the OutboundTransfers API.
        """

    class UpdateParamsOutboundTransfersAch(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class UpdateParamsOutboundTransfersUsDomesticWire(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    def update(
        self,
        financial_account: str,
        params: "FinancialAccountFeaturesService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> FinancialAccountFeatures:
        """
        Updates the Features associated with a FinancialAccount.
        """
        return cast(
            FinancialAccountFeatures,
            self._request(
                "post",
                "/v1/treasury/financial_accounts/{financial_account}/features".format(
                    financial_account=sanitize_id(financial_account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        financial_account: str,
        params: "FinancialAccountFeaturesService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> FinancialAccountFeatures:
        """
        Updates the Features associated with a FinancialAccount.
        """
        return cast(
            FinancialAccountFeatures,
            await self._request_async(
                "post",
                "/v1/treasury/financial_accounts/{financial_account}/features".format(
                    financial_account=sanitize_id(financial_account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        financial_account: str,
        params: "FinancialAccountFeaturesService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> FinancialAccountFeatures:
        """
        Retrieves Features information associated with the FinancialAccount.
        """
        return cast(
            FinancialAccountFeatures,
            self._request(
                "get",
                "/v1/treasury/financial_accounts/{financial_account}/features".format(
                    financial_account=sanitize_id(financial_account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        financial_account: str,
        params: "FinancialAccountFeaturesService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> FinancialAccountFeatures:
        """
        Retrieves Features information associated with the FinancialAccount.
        """
        return cast(
            FinancialAccountFeatures,
            await self._request_async(
                "get",
                "/v1/treasury/financial_accounts/{financial_account}/features".format(
                    financial_account=sanitize_id(financial_account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
