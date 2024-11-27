# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._dispute import Dispute
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class DisputeService(StripeService):
    class CloseParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ListParams(TypedDict):
        charge: NotRequired[str]
        """
        Only return disputes associated to the charge specified by this charge ID.
        """
        created: NotRequired["DisputeService.ListParamsCreated|int"]
        """
        Only return disputes that were created during the given date interval.
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
        payment_intent: NotRequired[str]
        """
        Only return disputes associated to the PaymentIntent specified by this PaymentIntent ID.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
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
        evidence: NotRequired["DisputeService.UpdateParamsEvidence"]
        """
        Evidence to upload, to respond to a dispute. Updating any field in the hash will submit all fields in the hash for review. The combined character count of all fields is limited to 150,000.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        submit: NotRequired[bool]
        """
        Whether to immediately submit evidence to the bank. If `false`, evidence is staged on the dispute. Staged evidence is visible in the API and Dashboard, and can be submitted to the bank by making another request with this attribute set to `true` (the default).
        """

    class UpdateParamsEvidence(TypedDict):
        access_activity_log: NotRequired[str]
        """
        Any server or activity logs showing proof that the customer accessed or downloaded the purchased digital product. This information should include IP addresses, corresponding timestamps, and any detailed recorded activity. Has a maximum character count of 20,000.
        """
        billing_address: NotRequired[str]
        """
        The billing address provided by the customer.
        """
        cancellation_policy: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Your subscription cancellation policy, as shown to the customer.
        """
        cancellation_policy_disclosure: NotRequired[str]
        """
        An explanation of how and when the customer was shown your refund policy prior to purchase. Has a maximum character count of 20,000.
        """
        cancellation_rebuttal: NotRequired[str]
        """
        A justification for why the customer's subscription was not canceled. Has a maximum character count of 20,000.
        """
        customer_communication: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Any communication with the customer that you feel is relevant to your case. Examples include emails proving that the customer received the product or service, or demonstrating their use of or satisfaction with the product or service.
        """
        customer_email_address: NotRequired[str]
        """
        The email address of the customer.
        """
        customer_name: NotRequired[str]
        """
        The name of the customer.
        """
        customer_purchase_ip: NotRequired[str]
        """
        The IP address that the customer used when making the purchase.
        """
        customer_signature: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) A relevant document or contract showing the customer's signature.
        """
        duplicate_charge_documentation: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Documentation for the prior charge that can uniquely identify the charge, such as a receipt, shipping label, work order, etc. This document should be paired with a similar document from the disputed payment that proves the two payments are separate.
        """
        duplicate_charge_explanation: NotRequired[str]
        """
        An explanation of the difference between the disputed charge versus the prior charge that appears to be a duplicate. Has a maximum character count of 20,000.
        """
        duplicate_charge_id: NotRequired[str]
        """
        The Stripe ID for the prior charge which appears to be a duplicate of the disputed charge.
        """
        product_description: NotRequired[str]
        """
        A description of the product or service that was sold. Has a maximum character count of 20,000.
        """
        receipt: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Any receipt or message sent to the customer notifying them of the charge.
        """
        refund_policy: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Your refund policy, as shown to the customer.
        """
        refund_policy_disclosure: NotRequired[str]
        """
        Documentation demonstrating that the customer was shown your refund policy prior to purchase. Has a maximum character count of 20,000.
        """
        refund_refusal_explanation: NotRequired[str]
        """
        A justification for why the customer is not entitled to a refund. Has a maximum character count of 20,000.
        """
        service_date: NotRequired[str]
        """
        The date on which the customer received or began receiving the purchased service, in a clear human-readable format.
        """
        service_documentation: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Documentation showing proof that a service was provided to the customer. This could include a copy of a signed contract, work order, or other form of written agreement.
        """
        shipping_address: NotRequired[str]
        """
        The address to which a physical product was shipped. You should try to include as complete address information as possible.
        """
        shipping_carrier: NotRequired[str]
        """
        The delivery service that shipped a physical product, such as Fedex, UPS, USPS, etc. If multiple carriers were used for this purchase, please separate them with commas.
        """
        shipping_date: NotRequired[str]
        """
        The date on which a physical product began its route to the shipping address, in a clear human-readable format.
        """
        shipping_documentation: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Documentation showing proof that a product was shipped to the customer at the same address the customer provided to you. This could include a copy of the shipment receipt, shipping label, etc. It should show the customer's full shipping address, if possible.
        """
        shipping_tracking_number: NotRequired[str]
        """
        The tracking number for a physical product, obtained from the delivery service. If multiple tracking numbers were generated for this purchase, please separate them with commas.
        """
        uncategorized_file: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) Any additional evidence or statements.
        """
        uncategorized_text: NotRequired[str]
        """
        Any additional evidence or statements. Has a maximum character count of 20,000.
        """

    def list(
        self,
        params: "DisputeService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Dispute]:
        """
        Returns a list of your disputes.
        """
        return cast(
            ListObject[Dispute],
            self._request(
                "get",
                "/v1/disputes",
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
        Returns a list of your disputes.
        """
        return cast(
            ListObject[Dispute],
            await self._request_async(
                "get",
                "/v1/disputes",
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
        Retrieves the dispute with the given ID.
        """
        return cast(
            Dispute,
            self._request(
                "get",
                "/v1/disputes/{dispute}".format(dispute=sanitize_id(dispute)),
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
        Retrieves the dispute with the given ID.
        """
        return cast(
            Dispute,
            await self._request_async(
                "get",
                "/v1/disputes/{dispute}".format(dispute=sanitize_id(dispute)),
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
        When you get a dispute, contacting your customer is always the best first step. If that doesn't work, you can submit evidence to help us resolve the dispute in your favor. You can do this in your [dashboard](https://dashboard.stripe.com/disputes), but if you prefer, you can use the API to submit evidence programmatically.

        Depending on your dispute type, different evidence fields will give you a better chance of winning your dispute. To figure out which evidence fields to provide, see our [guide to dispute types](https://stripe.com/docs/disputes/categories).
        """
        return cast(
            Dispute,
            self._request(
                "post",
                "/v1/disputes/{dispute}".format(dispute=sanitize_id(dispute)),
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
        When you get a dispute, contacting your customer is always the best first step. If that doesn't work, you can submit evidence to help us resolve the dispute in your favor. You can do this in your [dashboard](https://dashboard.stripe.com/disputes), but if you prefer, you can use the API to submit evidence programmatically.

        Depending on your dispute type, different evidence fields will give you a better chance of winning your dispute. To figure out which evidence fields to provide, see our [guide to dispute types](https://stripe.com/docs/disputes/categories).
        """
        return cast(
            Dispute,
            await self._request_async(
                "post",
                "/v1/disputes/{dispute}".format(dispute=sanitize_id(dispute)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def close(
        self,
        dispute: str,
        params: "DisputeService.CloseParams" = {},
        options: RequestOptions = {},
    ) -> Dispute:
        """
        Closing the dispute for a charge indicates that you do not have any evidence to submit and are essentially dismissing the dispute, acknowledging it as lost.

        The status of the dispute will change from needs_response to lost. Closing a dispute is irreversible.
        """
        return cast(
            Dispute,
            self._request(
                "post",
                "/v1/disputes/{dispute}/close".format(
                    dispute=sanitize_id(dispute),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def close_async(
        self,
        dispute: str,
        params: "DisputeService.CloseParams" = {},
        options: RequestOptions = {},
    ) -> Dispute:
        """
        Closing the dispute for a charge indicates that you do not have any evidence to submit and are essentially dismissing the dispute, acknowledging it as lost.

        The status of the dispute will change from needs_response to lost. Closing a dispute is irreversible.
        """
        return cast(
            Dispute,
            await self._request_async(
                "post",
                "/v1/disputes/{dispute}/close".format(
                    dispute=sanitize_id(dispute),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
