# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.treasury._financial_account import FinancialAccount
from stripe.treasury._financial_account_features_service import (
    FinancialAccountFeaturesService,
)
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class FinancialAccountService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.features = FinancialAccountFeaturesService(self._requestor)

    class CreateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        features: NotRequired["FinancialAccountService.CreateParamsFeatures"]
        """
        Encodes whether a FinancialAccount has access to a particular feature. Stripe or the platform can control features via the requested field.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        platform_restrictions: NotRequired[
            "FinancialAccountService.CreateParamsPlatformRestrictions"
        ]
        """
        The set of functionalities that the platform can restrict on the FinancialAccount.
        """
        supported_currencies: List[str]
        """
        The currencies the FinancialAccount can hold a balance in.
        """

    class CreateParamsFeatures(TypedDict):
        card_issuing: NotRequired[
            "FinancialAccountService.CreateParamsFeaturesCardIssuing"
        ]
        """
        Encodes the FinancialAccount's ability to be used with the Issuing product, including attaching cards to and drawing funds from the FinancialAccount.
        """
        deposit_insurance: NotRequired[
            "FinancialAccountService.CreateParamsFeaturesDepositInsurance"
        ]
        """
        Represents whether this FinancialAccount is eligible for deposit insurance. Various factors determine the insurance amount.
        """
        financial_addresses: NotRequired[
            "FinancialAccountService.CreateParamsFeaturesFinancialAddresses"
        ]
        """
        Contains Features that add FinancialAddresses to the FinancialAccount.
        """
        inbound_transfers: NotRequired[
            "FinancialAccountService.CreateParamsFeaturesInboundTransfers"
        ]
        """
        Contains settings related to adding funds to a FinancialAccount from another Account with the same owner.
        """
        intra_stripe_flows: NotRequired[
            "FinancialAccountService.CreateParamsFeaturesIntraStripeFlows"
        ]
        """
        Represents the ability for the FinancialAccount to send money to, or receive money from other FinancialAccounts (for example, via OutboundPayment).
        """
        outbound_payments: NotRequired[
            "FinancialAccountService.CreateParamsFeaturesOutboundPayments"
        ]
        """
        Includes Features related to initiating money movement out of the FinancialAccount to someone else's bucket of money.
        """
        outbound_transfers: NotRequired[
            "FinancialAccountService.CreateParamsFeaturesOutboundTransfers"
        ]
        """
        Contains a Feature and settings related to moving money out of the FinancialAccount into another Account with the same owner.
        """

    class CreateParamsFeaturesCardIssuing(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class CreateParamsFeaturesDepositInsurance(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class CreateParamsFeaturesFinancialAddresses(TypedDict):
        aba: NotRequired[
            "FinancialAccountService.CreateParamsFeaturesFinancialAddressesAba"
        ]
        """
        Adds an ABA FinancialAddress to the FinancialAccount.
        """

    class CreateParamsFeaturesFinancialAddressesAba(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class CreateParamsFeaturesInboundTransfers(TypedDict):
        ach: NotRequired[
            "FinancialAccountService.CreateParamsFeaturesInboundTransfersAch"
        ]
        """
        Enables ACH Debits via the InboundTransfers API.
        """

    class CreateParamsFeaturesInboundTransfersAch(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class CreateParamsFeaturesIntraStripeFlows(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class CreateParamsFeaturesOutboundPayments(TypedDict):
        ach: NotRequired[
            "FinancialAccountService.CreateParamsFeaturesOutboundPaymentsAch"
        ]
        """
        Enables ACH transfers via the OutboundPayments API.
        """
        us_domestic_wire: NotRequired[
            "FinancialAccountService.CreateParamsFeaturesOutboundPaymentsUsDomesticWire"
        ]
        """
        Enables US domestic wire transfers via the OutboundPayments API.
        """

    class CreateParamsFeaturesOutboundPaymentsAch(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class CreateParamsFeaturesOutboundPaymentsUsDomesticWire(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class CreateParamsFeaturesOutboundTransfers(TypedDict):
        ach: NotRequired[
            "FinancialAccountService.CreateParamsFeaturesOutboundTransfersAch"
        ]
        """
        Enables ACH transfers via the OutboundTransfers API.
        """
        us_domestic_wire: NotRequired[
            "FinancialAccountService.CreateParamsFeaturesOutboundTransfersUsDomesticWire"
        ]
        """
        Enables US domestic wire transfers via the OutboundTransfers API.
        """

    class CreateParamsFeaturesOutboundTransfersAch(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class CreateParamsFeaturesOutboundTransfersUsDomesticWire(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class CreateParamsPlatformRestrictions(TypedDict):
        inbound_flows: NotRequired[Literal["restricted", "unrestricted"]]
        """
        Restricts all inbound money movement.
        """
        outbound_flows: NotRequired[Literal["restricted", "unrestricted"]]
        """
        Restricts all outbound money movement.
        """

    class ListParams(TypedDict):
        created: NotRequired["FinancialAccountService.ListParamsCreated|int"]
        """
        Only return FinancialAccounts that were created during the given date interval.
        """
        ending_before: NotRequired[str]
        """
        An object ID cursor for use in pagination.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        limit: NotRequired[int]
        """
        A limit ranging from 1 to 100 (defaults to 10).
        """
        starting_after: NotRequired[str]
        """
        An object ID cursor for use in pagination.
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
        features: NotRequired["FinancialAccountService.UpdateParamsFeatures"]
        """
        Encodes whether a FinancialAccount has access to a particular feature, with a status enum and associated `status_details`. Stripe or the platform may control features via the requested field.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        platform_restrictions: NotRequired[
            "FinancialAccountService.UpdateParamsPlatformRestrictions"
        ]
        """
        The set of functionalities that the platform can restrict on the FinancialAccount.
        """

    class UpdateParamsFeatures(TypedDict):
        card_issuing: NotRequired[
            "FinancialAccountService.UpdateParamsFeaturesCardIssuing"
        ]
        """
        Encodes the FinancialAccount's ability to be used with the Issuing product, including attaching cards to and drawing funds from the FinancialAccount.
        """
        deposit_insurance: NotRequired[
            "FinancialAccountService.UpdateParamsFeaturesDepositInsurance"
        ]
        """
        Represents whether this FinancialAccount is eligible for deposit insurance. Various factors determine the insurance amount.
        """
        financial_addresses: NotRequired[
            "FinancialAccountService.UpdateParamsFeaturesFinancialAddresses"
        ]
        """
        Contains Features that add FinancialAddresses to the FinancialAccount.
        """
        inbound_transfers: NotRequired[
            "FinancialAccountService.UpdateParamsFeaturesInboundTransfers"
        ]
        """
        Contains settings related to adding funds to a FinancialAccount from another Account with the same owner.
        """
        intra_stripe_flows: NotRequired[
            "FinancialAccountService.UpdateParamsFeaturesIntraStripeFlows"
        ]
        """
        Represents the ability for the FinancialAccount to send money to, or receive money from other FinancialAccounts (for example, via OutboundPayment).
        """
        outbound_payments: NotRequired[
            "FinancialAccountService.UpdateParamsFeaturesOutboundPayments"
        ]
        """
        Includes Features related to initiating money movement out of the FinancialAccount to someone else's bucket of money.
        """
        outbound_transfers: NotRequired[
            "FinancialAccountService.UpdateParamsFeaturesOutboundTransfers"
        ]
        """
        Contains a Feature and settings related to moving money out of the FinancialAccount into another Account with the same owner.
        """

    class UpdateParamsFeaturesCardIssuing(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class UpdateParamsFeaturesDepositInsurance(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class UpdateParamsFeaturesFinancialAddresses(TypedDict):
        aba: NotRequired[
            "FinancialAccountService.UpdateParamsFeaturesFinancialAddressesAba"
        ]
        """
        Adds an ABA FinancialAddress to the FinancialAccount.
        """

    class UpdateParamsFeaturesFinancialAddressesAba(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class UpdateParamsFeaturesInboundTransfers(TypedDict):
        ach: NotRequired[
            "FinancialAccountService.UpdateParamsFeaturesInboundTransfersAch"
        ]
        """
        Enables ACH Debits via the InboundTransfers API.
        """

    class UpdateParamsFeaturesInboundTransfersAch(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class UpdateParamsFeaturesIntraStripeFlows(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class UpdateParamsFeaturesOutboundPayments(TypedDict):
        ach: NotRequired[
            "FinancialAccountService.UpdateParamsFeaturesOutboundPaymentsAch"
        ]
        """
        Enables ACH transfers via the OutboundPayments API.
        """
        us_domestic_wire: NotRequired[
            "FinancialAccountService.UpdateParamsFeaturesOutboundPaymentsUsDomesticWire"
        ]
        """
        Enables US domestic wire transfers via the OutboundPayments API.
        """

    class UpdateParamsFeaturesOutboundPaymentsAch(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class UpdateParamsFeaturesOutboundPaymentsUsDomesticWire(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class UpdateParamsFeaturesOutboundTransfers(TypedDict):
        ach: NotRequired[
            "FinancialAccountService.UpdateParamsFeaturesOutboundTransfersAch"
        ]
        """
        Enables ACH transfers via the OutboundTransfers API.
        """
        us_domestic_wire: NotRequired[
            "FinancialAccountService.UpdateParamsFeaturesOutboundTransfersUsDomesticWire"
        ]
        """
        Enables US domestic wire transfers via the OutboundTransfers API.
        """

    class UpdateParamsFeaturesOutboundTransfersAch(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class UpdateParamsFeaturesOutboundTransfersUsDomesticWire(TypedDict):
        requested: bool
        """
        Whether the FinancialAccount should have the Feature.
        """

    class UpdateParamsPlatformRestrictions(TypedDict):
        inbound_flows: NotRequired[Literal["restricted", "unrestricted"]]
        """
        Restricts all inbound money movement.
        """
        outbound_flows: NotRequired[Literal["restricted", "unrestricted"]]
        """
        Restricts all outbound money movement.
        """

    def list(
        self,
        params: "FinancialAccountService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[FinancialAccount]:
        """
        Returns a list of FinancialAccounts.
        """
        return cast(
            ListObject[FinancialAccount],
            self._request(
                "get",
                "/v1/treasury/financial_accounts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "FinancialAccountService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[FinancialAccount]:
        """
        Returns a list of FinancialAccounts.
        """
        return cast(
            ListObject[FinancialAccount],
            await self._request_async(
                "get",
                "/v1/treasury/financial_accounts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "FinancialAccountService.CreateParams",
        options: RequestOptions = {},
    ) -> FinancialAccount:
        """
        Creates a new FinancialAccount. For now, each connected account can only have one FinancialAccount.
        """
        return cast(
            FinancialAccount,
            self._request(
                "post",
                "/v1/treasury/financial_accounts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "FinancialAccountService.CreateParams",
        options: RequestOptions = {},
    ) -> FinancialAccount:
        """
        Creates a new FinancialAccount. For now, each connected account can only have one FinancialAccount.
        """
        return cast(
            FinancialAccount,
            await self._request_async(
                "post",
                "/v1/treasury/financial_accounts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        financial_account: str,
        params: "FinancialAccountService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> FinancialAccount:
        """
        Retrieves the details of a FinancialAccount.
        """
        return cast(
            FinancialAccount,
            self._request(
                "get",
                "/v1/treasury/financial_accounts/{financial_account}".format(
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
        params: "FinancialAccountService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> FinancialAccount:
        """
        Retrieves the details of a FinancialAccount.
        """
        return cast(
            FinancialAccount,
            await self._request_async(
                "get",
                "/v1/treasury/financial_accounts/{financial_account}".format(
                    financial_account=sanitize_id(financial_account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        financial_account: str,
        params: "FinancialAccountService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> FinancialAccount:
        """
        Updates the details of a FinancialAccount.
        """
        return cast(
            FinancialAccount,
            self._request(
                "post",
                "/v1/treasury/financial_accounts/{financial_account}".format(
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
        params: "FinancialAccountService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> FinancialAccount:
        """
        Updates the details of a FinancialAccount.
        """
        return cast(
            FinancialAccount,
            await self._request_async(
                "post",
                "/v1/treasury/financial_accounts/{financial_account}".format(
                    financial_account=sanitize_id(financial_account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
