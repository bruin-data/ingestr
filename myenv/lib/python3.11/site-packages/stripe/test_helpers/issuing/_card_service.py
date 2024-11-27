# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.issuing._card import Card
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class CardService(StripeService):
    class DeliverCardParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class FailCardParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ReturnCardParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ShipCardParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def deliver_card(
        self,
        card: str,
        params: "CardService.DeliverCardParams" = {},
        options: RequestOptions = {},
    ) -> Card:
        """
        Updates the shipping status of the specified Issuing Card object to delivered.
        """
        return cast(
            Card,
            self._request(
                "post",
                "/v1/test_helpers/issuing/cards/{card}/shipping/deliver".format(
                    card=sanitize_id(card),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def deliver_card_async(
        self,
        card: str,
        params: "CardService.DeliverCardParams" = {},
        options: RequestOptions = {},
    ) -> Card:
        """
        Updates the shipping status of the specified Issuing Card object to delivered.
        """
        return cast(
            Card,
            await self._request_async(
                "post",
                "/v1/test_helpers/issuing/cards/{card}/shipping/deliver".format(
                    card=sanitize_id(card),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def fail_card(
        self,
        card: str,
        params: "CardService.FailCardParams" = {},
        options: RequestOptions = {},
    ) -> Card:
        """
        Updates the shipping status of the specified Issuing Card object to failure.
        """
        return cast(
            Card,
            self._request(
                "post",
                "/v1/test_helpers/issuing/cards/{card}/shipping/fail".format(
                    card=sanitize_id(card),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def fail_card_async(
        self,
        card: str,
        params: "CardService.FailCardParams" = {},
        options: RequestOptions = {},
    ) -> Card:
        """
        Updates the shipping status of the specified Issuing Card object to failure.
        """
        return cast(
            Card,
            await self._request_async(
                "post",
                "/v1/test_helpers/issuing/cards/{card}/shipping/fail".format(
                    card=sanitize_id(card),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def return_card(
        self,
        card: str,
        params: "CardService.ReturnCardParams" = {},
        options: RequestOptions = {},
    ) -> Card:
        """
        Updates the shipping status of the specified Issuing Card object to returned.
        """
        return cast(
            Card,
            self._request(
                "post",
                "/v1/test_helpers/issuing/cards/{card}/shipping/return".format(
                    card=sanitize_id(card),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def return_card_async(
        self,
        card: str,
        params: "CardService.ReturnCardParams" = {},
        options: RequestOptions = {},
    ) -> Card:
        """
        Updates the shipping status of the specified Issuing Card object to returned.
        """
        return cast(
            Card,
            await self._request_async(
                "post",
                "/v1/test_helpers/issuing/cards/{card}/shipping/return".format(
                    card=sanitize_id(card),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def ship_card(
        self,
        card: str,
        params: "CardService.ShipCardParams" = {},
        options: RequestOptions = {},
    ) -> Card:
        """
        Updates the shipping status of the specified Issuing Card object to shipped.
        """
        return cast(
            Card,
            self._request(
                "post",
                "/v1/test_helpers/issuing/cards/{card}/shipping/ship".format(
                    card=sanitize_id(card),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def ship_card_async(
        self,
        card: str,
        params: "CardService.ShipCardParams" = {},
        options: RequestOptions = {},
    ) -> Card:
        """
        Updates the shipping status of the specified Issuing Card object to shipped.
        """
        return cast(
            Card,
            await self._request_async(
                "post",
                "/v1/test_helpers/issuing/cards/{card}/shipping/ship".format(
                    card=sanitize_id(card),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
