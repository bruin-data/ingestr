# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.issuing._personalization_design import PersonalizationDesign
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class PersonalizationDesignService(StripeService):
    class ActivateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class DeactivateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class RejectParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        rejection_reasons: (
            "PersonalizationDesignService.RejectParamsRejectionReasons"
        )
        """
        The reason(s) the personalization design was rejected.
        """

    class RejectParamsRejectionReasons(TypedDict):
        card_logo: NotRequired[
            List[
                Literal[
                    "geographic_location",
                    "inappropriate",
                    "network_name",
                    "non_binary_image",
                    "non_fiat_currency",
                    "other",
                    "other_entity",
                    "promotional_material",
                ]
            ]
        ]
        """
        The reason(s) the card logo was rejected.
        """
        carrier_text: NotRequired[
            List[
                Literal[
                    "geographic_location",
                    "inappropriate",
                    "network_name",
                    "non_fiat_currency",
                    "other",
                    "other_entity",
                    "promotional_material",
                ]
            ]
        ]
        """
        The reason(s) the carrier text was rejected.
        """

    def activate(
        self,
        personalization_design: str,
        params: "PersonalizationDesignService.ActivateParams" = {},
        options: RequestOptions = {},
    ) -> PersonalizationDesign:
        """
        Updates the status of the specified testmode personalization design object to active.
        """
        return cast(
            PersonalizationDesign,
            self._request(
                "post",
                "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/activate".format(
                    personalization_design=sanitize_id(personalization_design),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def activate_async(
        self,
        personalization_design: str,
        params: "PersonalizationDesignService.ActivateParams" = {},
        options: RequestOptions = {},
    ) -> PersonalizationDesign:
        """
        Updates the status of the specified testmode personalization design object to active.
        """
        return cast(
            PersonalizationDesign,
            await self._request_async(
                "post",
                "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/activate".format(
                    personalization_design=sanitize_id(personalization_design),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def deactivate(
        self,
        personalization_design: str,
        params: "PersonalizationDesignService.DeactivateParams" = {},
        options: RequestOptions = {},
    ) -> PersonalizationDesign:
        """
        Updates the status of the specified testmode personalization design object to inactive.
        """
        return cast(
            PersonalizationDesign,
            self._request(
                "post",
                "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/deactivate".format(
                    personalization_design=sanitize_id(personalization_design),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def deactivate_async(
        self,
        personalization_design: str,
        params: "PersonalizationDesignService.DeactivateParams" = {},
        options: RequestOptions = {},
    ) -> PersonalizationDesign:
        """
        Updates the status of the specified testmode personalization design object to inactive.
        """
        return cast(
            PersonalizationDesign,
            await self._request_async(
                "post",
                "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/deactivate".format(
                    personalization_design=sanitize_id(personalization_design),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def reject(
        self,
        personalization_design: str,
        params: "PersonalizationDesignService.RejectParams",
        options: RequestOptions = {},
    ) -> PersonalizationDesign:
        """
        Updates the status of the specified testmode personalization design object to rejected.
        """
        return cast(
            PersonalizationDesign,
            self._request(
                "post",
                "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/reject".format(
                    personalization_design=sanitize_id(personalization_design),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def reject_async(
        self,
        personalization_design: str,
        params: "PersonalizationDesignService.RejectParams",
        options: RequestOptions = {},
    ) -> PersonalizationDesign:
        """
        Updates the status of the specified testmode personalization design object to rejected.
        """
        return cast(
            PersonalizationDesign,
            await self._request_async(
                "post",
                "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/reject".format(
                    personalization_design=sanitize_id(personalization_design),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
