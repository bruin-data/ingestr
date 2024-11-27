# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.issuing._dispute import Dispute
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class DisputeService(StripeService):
    class CreateParams(TypedDict):
        amount: NotRequired[int]
        """
        The dispute amount in the card's currency and in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal). If not set, defaults to the full transaction amount.
        """
        evidence: NotRequired["DisputeService.CreateParamsEvidence"]
        """
        Evidence provided for the dispute.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        transaction: NotRequired[str]
        """
        The ID of the issuing transaction to create a dispute for. For transaction on Treasury FinancialAccounts, use `treasury.received_debit`.
        """
        treasury: NotRequired["DisputeService.CreateParamsTreasury"]
        """
        Params for disputes related to Treasury FinancialAccounts
        """

    class CreateParamsEvidence(TypedDict):
        canceled: NotRequired[
            "Literal['']|DisputeService.CreateParamsEvidenceCanceled"
        ]
        """
        Evidence provided when `reason` is 'canceled'.
        """
        duplicate: NotRequired[
            "Literal['']|DisputeService.CreateParamsEvidenceDuplicate"
        ]
        """
        Evidence provided when `reason` is 'duplicate'.
        """
        fraudulent: NotRequired[
            "Literal['']|DisputeService.CreateParamsEvidenceFraudulent"
        ]
        """
        Evidence provided when `reason` is 'fraudulent'.
        """
        merchandise_not_as_described: NotRequired[
            "Literal['']|DisputeService.CreateParamsEvidenceMerchandiseNotAsDescribed"
        ]
        """
        Evidence provided when `reason` is 'merchandise_not_as_described'.
        """
        no_valid_authorization: NotRequired[
            "Literal['']|DisputeService.CreateParamsEvidenceNoValidAuthorization"
        ]
        """
        Evidence provided when `reason` is 'no_valid_authorization'.
        """
        not_received: NotRequired[
            "Literal['']|DisputeService.CreateParamsEvidenceNotReceived"
        ]
        """
        Evidence provided when `reason` is 'not_received'.
        """
        other: NotRequired[
            "Literal['']|DisputeService.CreateParamsEvidenceOther"
        ]
        """
        Evidence provided when `reason` is 'other'.
        """
        reason: NotRequired[
            Literal[
                "canceled",
                "duplicate",
                "fraudulent",
                "merchandise_not_as_described",
                "no_valid_authorization",
                "not_received",
                "other",
                "service_not_as_described",
            ]
        ]
        """
        The reason for filing the dispute. The evidence should be submitted in the field of the same name.
        """
        service_not_as_described: NotRequired[
            "Literal['']|DisputeService.CreateParamsEvidenceServiceNotAsDescribed"
        ]
        """
        Evidence provided when `reason` is 'service_not_as_described'.
        """

    class CreateParamsEvidenceCanceled(TypedDict):
        additional_documentation: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Additional documentation supporting the dispute.
        """
        canceled_at: NotRequired["Literal['']|int"]
        """
        Date when order was canceled.
        """
        cancellation_policy_provided: NotRequired["Literal['']|bool"]
        """
        Whether the cardholder was provided with a cancellation policy.
        """
        cancellation_reason: NotRequired["Literal['']|str"]
        """
        Reason for canceling the order.
        """
        expected_at: NotRequired["Literal['']|int"]
        """
        Date when the cardholder expected to receive the product.
        """
        explanation: NotRequired["Literal['']|str"]
        """
        Explanation of why the cardholder is disputing this transaction.
        """
        product_description: NotRequired["Literal['']|str"]
        """
        Description of the merchandise or service that was purchased.
        """
        product_type: NotRequired[
            "Literal['']|Literal['merchandise', 'service']"
        ]
        """
        Whether the product was a merchandise or service.
        """
        return_status: NotRequired[
            "Literal['']|Literal['merchant_rejected', 'successful']"
        ]
        """
        Result of cardholder's attempt to return the product.
        """
        returned_at: NotRequired["Literal['']|int"]
        """
        Date when the product was returned or attempted to be returned.
        """

    class CreateParamsEvidenceDuplicate(TypedDict):
        additional_documentation: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Additional documentation supporting the dispute.
        """
        card_statement: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Copy of the card statement showing that the product had already been paid for.
        """
        cash_receipt: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Copy of the receipt showing that the product had been paid for in cash.
        """
        check_image: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Image of the front and back of the check that was used to pay for the product.
        """
        explanation: NotRequired["Literal['']|str"]
        """
        Explanation of why the cardholder is disputing this transaction.
        """
        original_transaction: NotRequired[str]
        """
        Transaction (e.g., ipi_...) that the disputed transaction is a duplicate of. Of the two or more transactions that are copies of each other, this is original undisputed one.
        """

    class CreateParamsEvidenceFraudulent(TypedDict):
        additional_documentation: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Additional documentation supporting the dispute.
        """
        explanation: NotRequired["Literal['']|str"]
        """
        Explanation of why the cardholder is disputing this transaction.
        """

    class CreateParamsEvidenceMerchandiseNotAsDescribed(TypedDict):
        additional_documentation: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Additional documentation supporting the dispute.
        """
        explanation: NotRequired["Literal['']|str"]
        """
        Explanation of why the cardholder is disputing this transaction.
        """
        received_at: NotRequired["Literal['']|int"]
        """
        Date when the product was received.
        """
        return_description: NotRequired["Literal['']|str"]
        """
        Description of the cardholder's attempt to return the product.
        """
        return_status: NotRequired[
            "Literal['']|Literal['merchant_rejected', 'successful']"
        ]
        """
        Result of cardholder's attempt to return the product.
        """
        returned_at: NotRequired["Literal['']|int"]
        """
        Date when the product was returned or attempted to be returned.
        """

    class CreateParamsEvidenceNoValidAuthorization(TypedDict):
        additional_documentation: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Additional documentation supporting the dispute.
        """
        explanation: NotRequired["Literal['']|str"]
        """
        Explanation of why the cardholder is disputing this transaction.
        """

    class CreateParamsEvidenceNotReceived(TypedDict):
        additional_documentation: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Additional documentation supporting the dispute.
        """
        expected_at: NotRequired["Literal['']|int"]
        """
        Date when the cardholder expected to receive the product.
        """
        explanation: NotRequired["Literal['']|str"]
        """
        Explanation of why the cardholder is disputing this transaction.
        """
        product_description: NotRequired["Literal['']|str"]
        """
        Description of the merchandise or service that was purchased.
        """
        product_type: NotRequired[
            "Literal['']|Literal['merchandise', 'service']"
        ]
        """
        Whether the product was a merchandise or service.
        """

    class CreateParamsEvidenceOther(TypedDict):
        additional_documentation: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Additional documentation supporting the dispute.
        """
        explanation: NotRequired["Literal['']|str"]
        """
        Explanation of why the cardholder is disputing this transaction.
        """
        product_description: NotRequired["Literal['']|str"]
        """
        Description of the merchandise or service that was purchased.
        """
        product_type: NotRequired[
            "Literal['']|Literal['merchandise', 'service']"
        ]
        """
        Whether the product was a merchandise or service.
        """

    class CreateParamsEvidenceServiceNotAsDescribed(TypedDict):
        additional_documentation: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Additional documentation supporting the dispute.
        """
        canceled_at: NotRequired["Literal['']|int"]
        """
        Date when order was canceled.
        """
        cancellation_reason: NotRequired["Literal['']|str"]
        """
        Reason for canceling the order.
        """
        explanation: NotRequired["Literal['']|str"]
        """
        Explanation of why the cardholder is disputing this transaction.
        """
        received_at: NotRequired["Literal['']|int"]
        """
        Date when the product was received.
        """

    class CreateParamsTreasury(TypedDict):
        received_debit: str
        """
        The ID of the ReceivedDebit to initiate an Issuings dispute for.
        """

    class ListParams(TypedDict):
        created: NotRequired["DisputeService.ListParamsCreated|int"]
        """
        Only return Issuing disputes that were created during the given date interval.
        """
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
        status: NotRequired[
            Literal["expired", "lost", "submitted", "unsubmitted", "won"]
        ]
        """
        Select Issuing disputes with the given status.
        """
        transaction: NotRequired[str]
        """
        Select the Issuing dispute for the given transaction.
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

    class SubmitParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class UpdateParams(TypedDict):
        amount: NotRequired[int]
        """
        The dispute amount in the card's currency and in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal).
        """
        evidence: NotRequired["DisputeService.UpdateParamsEvidence"]
        """
        Evidence provided for the dispute.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class UpdateParamsEvidence(TypedDict):
        canceled: NotRequired[
            "Literal['']|DisputeService.UpdateParamsEvidenceCanceled"
        ]
        """
        Evidence provided when `reason` is 'canceled'.
        """
        duplicate: NotRequired[
            "Literal['']|DisputeService.UpdateParamsEvidenceDuplicate"
        ]
        """
        Evidence provided when `reason` is 'duplicate'.
        """
        fraudulent: NotRequired[
            "Literal['']|DisputeService.UpdateParamsEvidenceFraudulent"
        ]
        """
        Evidence provided when `reason` is 'fraudulent'.
        """
        merchandise_not_as_described: NotRequired[
            "Literal['']|DisputeService.UpdateParamsEvidenceMerchandiseNotAsDescribed"
        ]
        """
        Evidence provided when `reason` is 'merchandise_not_as_described'.
        """
        no_valid_authorization: NotRequired[
            "Literal['']|DisputeService.UpdateParamsEvidenceNoValidAuthorization"
        ]
        """
        Evidence provided when `reason` is 'no_valid_authorization'.
        """
        not_received: NotRequired[
            "Literal['']|DisputeService.UpdateParamsEvidenceNotReceived"
        ]
        """
        Evidence provided when `reason` is 'not_received'.
        """
        other: NotRequired[
            "Literal['']|DisputeService.UpdateParamsEvidenceOther"
        ]
        """
        Evidence provided when `reason` is 'other'.
        """
        reason: NotRequired[
            Literal[
                "canceled",
                "duplicate",
                "fraudulent",
                "merchandise_not_as_described",
                "no_valid_authorization",
                "not_received",
                "other",
                "service_not_as_described",
            ]
        ]
        """
        The reason for filing the dispute. The evidence should be submitted in the field of the same name.
        """
        service_not_as_described: NotRequired[
            "Literal['']|DisputeService.UpdateParamsEvidenceServiceNotAsDescribed"
        ]
        """
        Evidence provided when `reason` is 'service_not_as_described'.
        """

    class UpdateParamsEvidenceCanceled(TypedDict):
        additional_documentation: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Additional documentation supporting the dispute.
        """
        canceled_at: NotRequired["Literal['']|int"]
        """
        Date when order was canceled.
        """
        cancellation_policy_provided: NotRequired["Literal['']|bool"]
        """
        Whether the cardholder was provided with a cancellation policy.
        """
        cancellation_reason: NotRequired["Literal['']|str"]
        """
        Reason for canceling the order.
        """
        expected_at: NotRequired["Literal['']|int"]
        """
        Date when the cardholder expected to receive the product.
        """
        explanation: NotRequired["Literal['']|str"]
        """
        Explanation of why the cardholder is disputing this transaction.
        """
        product_description: NotRequired["Literal['']|str"]
        """
        Description of the merchandise or service that was purchased.
        """
        product_type: NotRequired[
            "Literal['']|Literal['merchandise', 'service']"
        ]
        """
        Whether the product was a merchandise or service.
        """
        return_status: NotRequired[
            "Literal['']|Literal['merchant_rejected', 'successful']"
        ]
        """
        Result of cardholder's attempt to return the product.
        """
        returned_at: NotRequired["Literal['']|int"]
        """
        Date when the product was returned or attempted to be returned.
        """

    class UpdateParamsEvidenceDuplicate(TypedDict):
        additional_documentation: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Additional documentation supporting the dispute.
        """
        card_statement: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Copy of the card statement showing that the product had already been paid for.
        """
        cash_receipt: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Copy of the receipt showing that the product had been paid for in cash.
        """
        check_image: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Image of the front and back of the check that was used to pay for the product.
        """
        explanation: NotRequired["Literal['']|str"]
        """
        Explanation of why the cardholder is disputing this transaction.
        """
        original_transaction: NotRequired[str]
        """
        Transaction (e.g., ipi_...) that the disputed transaction is a duplicate of. Of the two or more transactions that are copies of each other, this is original undisputed one.
        """

    class UpdateParamsEvidenceFraudulent(TypedDict):
        additional_documentation: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Additional documentation supporting the dispute.
        """
        explanation: NotRequired["Literal['']|str"]
        """
        Explanation of why the cardholder is disputing this transaction.
        """

    class UpdateParamsEvidenceMerchandiseNotAsDescribed(TypedDict):
        additional_documentation: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Additional documentation supporting the dispute.
        """
        explanation: NotRequired["Literal['']|str"]
        """
        Explanation of why the cardholder is disputing this transaction.
        """
        received_at: NotRequired["Literal['']|int"]
        """
        Date when the product was received.
        """
        return_description: NotRequired["Literal['']|str"]
        """
        Description of the cardholder's attempt to return the product.
        """
        return_status: NotRequired[
            "Literal['']|Literal['merchant_rejected', 'successful']"
        ]
        """
        Result of cardholder's attempt to return the product.
        """
        returned_at: NotRequired["Literal['']|int"]
        """
        Date when the product was returned or attempted to be returned.
        """

    class UpdateParamsEvidenceNoValidAuthorization(TypedDict):
        additional_documentation: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Additional documentation supporting the dispute.
        """
        explanation: NotRequired["Literal['']|str"]
        """
        Explanation of why the cardholder is disputing this transaction.
        """

    class UpdateParamsEvidenceNotReceived(TypedDict):
        additional_documentation: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Additional documentation supporting the dispute.
        """
        expected_at: NotRequired["Literal['']|int"]
        """
        Date when the cardholder expected to receive the product.
        """
        explanation: NotRequired["Literal['']|str"]
        """
        Explanation of why the cardholder is disputing this transaction.
        """
        product_description: NotRequired["Literal['']|str"]
        """
        Description of the merchandise or service that was purchased.
        """
        product_type: NotRequired[
            "Literal['']|Literal['merchandise', 'service']"
        ]
        """
        Whether the product was a merchandise or service.
        """

    class UpdateParamsEvidenceOther(TypedDict):
        additional_documentation: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Additional documentation supporting the dispute.
        """
        explanation: NotRequired["Literal['']|str"]
        """
        Explanation of why the cardholder is disputing this transaction.
        """
        product_description: NotRequired["Literal['']|str"]
        """
        Description of the merchandise or service that was purchased.
        """
        product_type: NotRequired[
            "Literal['']|Literal['merchandise', 'service']"
        ]
        """
        Whether the product was a merchandise or service.
        """

    class UpdateParamsEvidenceServiceNotAsDescribed(TypedDict):
        additional_documentation: NotRequired["Literal['']|str"]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Additional documentation supporting the dispute.
        """
        canceled_at: NotRequired["Literal['']|int"]
        """
        Date when order was canceled.
        """
        cancellation_reason: NotRequired["Literal['']|str"]
        """
        Reason for canceling the order.
        """
        explanation: NotRequired["Literal['']|str"]
        """
        Explanation of why the cardholder is disputing this transaction.
        """
        received_at: NotRequired["Literal['']|int"]
        """
        Date when the product was received.
        """

    def list(
        self,
        params: "DisputeService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Dispute]:
        """
        Returns a list of Issuing Dispute objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        return cast(
            ListObject[Dispute],
            self._request(
                "get",
                "/v1/issuing/disputes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "DisputeService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Dispute]:
        """
        Returns a list of Issuing Dispute objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        return cast(
            ListObject[Dispute],
            await self._request_async(
                "get",
                "/v1/issuing/disputes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "DisputeService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Dispute:
        """
        Creates an Issuing Dispute object. Individual pieces of evidence within the evidence object are optional at this point. Stripe only validates that required evidence is present during submission. Refer to [Dispute reasons and evidence](https://stripe.com/docs/issuing/purchases/disputes#dispute-reasons-and-evidence) for more details about evidence requirements.
        """
        return cast(
            Dispute,
            self._request(
                "post",
                "/v1/issuing/disputes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "DisputeService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Dispute:
        """
        Creates an Issuing Dispute object. Individual pieces of evidence within the evidence object are optional at this point. Stripe only validates that required evidence is present during submission. Refer to [Dispute reasons and evidence](https://stripe.com/docs/issuing/purchases/disputes#dispute-reasons-and-evidence) for more details about evidence requirements.
        """
        return cast(
            Dispute,
            await self._request_async(
                "post",
                "/v1/issuing/disputes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        dispute: str,
        params: "DisputeService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Dispute:
        """
        Retrieves an Issuing Dispute object.
        """
        return cast(
            Dispute,
            self._request(
                "get",
                "/v1/issuing/disputes/{dispute}".format(
                    dispute=sanitize_id(dispute),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        dispute: str,
        params: "DisputeService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Dispute:
        """
        Retrieves an Issuing Dispute object.
        """
        return cast(
            Dispute,
            await self._request_async(
                "get",
                "/v1/issuing/disputes/{dispute}".format(
                    dispute=sanitize_id(dispute),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        dispute: str,
        params: "DisputeService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Dispute:
        """
        Updates the specified Issuing Dispute object by setting the values of the parameters passed. Any parameters not provided will be left unchanged. Properties on the evidence object can be unset by passing in an empty string.
        """
        return cast(
            Dispute,
            self._request(
                "post",
                "/v1/issuing/disputes/{dispute}".format(
                    dispute=sanitize_id(dispute),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        dispute: str,
        params: "DisputeService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Dispute:
        """
        Updates the specified Issuing Dispute object by setting the values of the parameters passed. Any parameters not provided will be left unchanged. Properties on the evidence object can be unset by passing in an empty string.
        """
        return cast(
            Dispute,
            await self._request_async(
                "post",
                "/v1/issuing/disputes/{dispute}".format(
                    dispute=sanitize_id(dispute),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def submit(
        self,
        dispute: str,
        params: "DisputeService.SubmitParams" = {},
        options: RequestOptions = {},
    ) -> Dispute:
        """
        Submits an Issuing Dispute to the card network. Stripe validates that all evidence fields required for the dispute's reason are present. For more details, see [Dispute reasons and evidence](https://stripe.com/docs/issuing/purchases/disputes#dispute-reasons-and-evidence).
        """
        return cast(
            Dispute,
            self._request(
                "post",
                "/v1/issuing/disputes/{dispute}/submit".format(
                    dispute=sanitize_id(dispute),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def submit_async(
        self,
        dispute: str,
        params: "DisputeService.SubmitParams" = {},
        options: RequestOptions = {},
    ) -> Dispute:
        """
        Submits an Issuing Dispute to the card network. Stripe validates that all evidence fields required for the dispute's reason are present. For more details, see [Dispute reasons and evidence](https://stripe.com/docs/issuing/purchases/disputes#dispute-reasons-and-evidence).
        """
        return cast(
            Dispute,
            await self._request_async(
                "post",
                "/v1/issuing/disputes/{dispute}/submit".format(
                    dispute=sanitize_id(dispute),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
