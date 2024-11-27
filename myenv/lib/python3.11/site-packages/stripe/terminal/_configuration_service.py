# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.terminal._configuration import Configuration
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class ConfigurationService(StripeService):
    class CreateParams(TypedDict):
        bbpos_wisepos_e: NotRequired[
            "ConfigurationService.CreateParamsBbposWiseposE"
        ]
        """
        An object containing device type specific settings for BBPOS WisePOS E readers
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        name: NotRequired[str]
        """
        Name of the configuration
        """
        offline: NotRequired[
            "Literal['']|ConfigurationService.CreateParamsOffline"
        ]
        """
        Configurations for collecting transactions offline.
        """
        reboot_window: NotRequired[
            "ConfigurationService.CreateParamsRebootWindow"
        ]
        """
        Reboot time settings for readers that support customized reboot time configuration.
        """
        stripe_s700: NotRequired["ConfigurationService.CreateParamsStripeS700"]
        """
        An object containing device type specific settings for Stripe S700 readers
        """
        tipping: NotRequired[
            "Literal['']|ConfigurationService.CreateParamsTipping"
        ]
        """
        Tipping configurations for readers supporting on-reader tips
        """
        verifone_p400: NotRequired[
            "ConfigurationService.CreateParamsVerifoneP400"
        ]
        """
        An object containing device type specific settings for Verifone P400 readers
        """

    class CreateParamsBbposWiseposE(TypedDict):
        splashscreen: NotRequired["Literal['']|str"]
        """
        A File ID representing an image you would like displayed on the reader.
        """

    class CreateParamsOffline(TypedDict):
        enabled: bool
        """
        Determines whether to allow transactions to be collected while reader is offline. Defaults to false.
        """

    class CreateParamsRebootWindow(TypedDict):
        end_hour: int
        """
        Integer between 0 to 23 that represents the end hour of the reboot time window. The value must be different than the start_hour.
        """
        start_hour: int
        """
        Integer between 0 to 23 that represents the start hour of the reboot time window.
        """

    class CreateParamsStripeS700(TypedDict):
        splashscreen: NotRequired["Literal['']|str"]
        """
        A File ID representing an image you would like displayed on the reader.
        """

    class CreateParamsTipping(TypedDict):
        aud: NotRequired["ConfigurationService.CreateParamsTippingAud"]
        """
        Tipping configuration for AUD
        """
        cad: NotRequired["ConfigurationService.CreateParamsTippingCad"]
        """
        Tipping configuration for CAD
        """
        chf: NotRequired["ConfigurationService.CreateParamsTippingChf"]
        """
        Tipping configuration for CHF
        """
        czk: NotRequired["ConfigurationService.CreateParamsTippingCzk"]
        """
        Tipping configuration for CZK
        """
        dkk: NotRequired["ConfigurationService.CreateParamsTippingDkk"]
        """
        Tipping configuration for DKK
        """
        eur: NotRequired["ConfigurationService.CreateParamsTippingEur"]
        """
        Tipping configuration for EUR
        """
        gbp: NotRequired["ConfigurationService.CreateParamsTippingGbp"]
        """
        Tipping configuration for GBP
        """
        hkd: NotRequired["ConfigurationService.CreateParamsTippingHkd"]
        """
        Tipping configuration for HKD
        """
        myr: NotRequired["ConfigurationService.CreateParamsTippingMyr"]
        """
        Tipping configuration for MYR
        """
        nok: NotRequired["ConfigurationService.CreateParamsTippingNok"]
        """
        Tipping configuration for NOK
        """
        nzd: NotRequired["ConfigurationService.CreateParamsTippingNzd"]
        """
        Tipping configuration for NZD
        """
        sek: NotRequired["ConfigurationService.CreateParamsTippingSek"]
        """
        Tipping configuration for SEK
        """
        sgd: NotRequired["ConfigurationService.CreateParamsTippingSgd"]
        """
        Tipping configuration for SGD
        """
        usd: NotRequired["ConfigurationService.CreateParamsTippingUsd"]
        """
        Tipping configuration for USD
        """

    class CreateParamsTippingAud(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingCad(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingChf(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingCzk(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingDkk(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingEur(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingGbp(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingHkd(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingMyr(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingNok(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingNzd(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingSek(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingSgd(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingUsd(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsVerifoneP400(TypedDict):
        splashscreen: NotRequired["Literal['']|str"]
        """
        A File ID representing an image you would like displayed on the reader.
        """

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
        is_account_default: NotRequired[bool]
        """
        if present, only return the account default or non-default configurations.
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

    class UpdateParams(TypedDict):
        bbpos_wisepos_e: NotRequired[
            "Literal['']|ConfigurationService.UpdateParamsBbposWiseposE"
        ]
        """
        An object containing device type specific settings for BBPOS WisePOS E readers
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        name: NotRequired[str]
        """
        Name of the configuration
        """
        offline: NotRequired[
            "Literal['']|ConfigurationService.UpdateParamsOffline"
        ]
        """
        Configurations for collecting transactions offline.
        """
        reboot_window: NotRequired[
            "Literal['']|ConfigurationService.UpdateParamsRebootWindow"
        ]
        """
        Reboot time settings for readers that support customized reboot time configuration.
        """
        stripe_s700: NotRequired[
            "Literal['']|ConfigurationService.UpdateParamsStripeS700"
        ]
        """
        An object containing device type specific settings for Stripe S700 readers
        """
        tipping: NotRequired[
            "Literal['']|ConfigurationService.UpdateParamsTipping"
        ]
        """
        Tipping configurations for readers supporting on-reader tips
        """
        verifone_p400: NotRequired[
            "Literal['']|ConfigurationService.UpdateParamsVerifoneP400"
        ]
        """
        An object containing device type specific settings for Verifone P400 readers
        """

    class UpdateParamsBbposWiseposE(TypedDict):
        splashscreen: NotRequired["Literal['']|str"]
        """
        A File ID representing an image you would like displayed on the reader.
        """

    class UpdateParamsOffline(TypedDict):
        enabled: bool
        """
        Determines whether to allow transactions to be collected while reader is offline. Defaults to false.
        """

    class UpdateParamsRebootWindow(TypedDict):
        end_hour: int
        """
        Integer between 0 to 23 that represents the end hour of the reboot time window. The value must be different than the start_hour.
        """
        start_hour: int
        """
        Integer between 0 to 23 that represents the start hour of the reboot time window.
        """

    class UpdateParamsStripeS700(TypedDict):
        splashscreen: NotRequired["Literal['']|str"]
        """
        A File ID representing an image you would like displayed on the reader.
        """

    class UpdateParamsTipping(TypedDict):
        aud: NotRequired["ConfigurationService.UpdateParamsTippingAud"]
        """
        Tipping configuration for AUD
        """
        cad: NotRequired["ConfigurationService.UpdateParamsTippingCad"]
        """
        Tipping configuration for CAD
        """
        chf: NotRequired["ConfigurationService.UpdateParamsTippingChf"]
        """
        Tipping configuration for CHF
        """
        czk: NotRequired["ConfigurationService.UpdateParamsTippingCzk"]
        """
        Tipping configuration for CZK
        """
        dkk: NotRequired["ConfigurationService.UpdateParamsTippingDkk"]
        """
        Tipping configuration for DKK
        """
        eur: NotRequired["ConfigurationService.UpdateParamsTippingEur"]
        """
        Tipping configuration for EUR
        """
        gbp: NotRequired["ConfigurationService.UpdateParamsTippingGbp"]
        """
        Tipping configuration for GBP
        """
        hkd: NotRequired["ConfigurationService.UpdateParamsTippingHkd"]
        """
        Tipping configuration for HKD
        """
        myr: NotRequired["ConfigurationService.UpdateParamsTippingMyr"]
        """
        Tipping configuration for MYR
        """
        nok: NotRequired["ConfigurationService.UpdateParamsTippingNok"]
        """
        Tipping configuration for NOK
        """
        nzd: NotRequired["ConfigurationService.UpdateParamsTippingNzd"]
        """
        Tipping configuration for NZD
        """
        sek: NotRequired["ConfigurationService.UpdateParamsTippingSek"]
        """
        Tipping configuration for SEK
        """
        sgd: NotRequired["ConfigurationService.UpdateParamsTippingSgd"]
        """
        Tipping configuration for SGD
        """
        usd: NotRequired["ConfigurationService.UpdateParamsTippingUsd"]
        """
        Tipping configuration for USD
        """

    class UpdateParamsTippingAud(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class UpdateParamsTippingCad(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class UpdateParamsTippingChf(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class UpdateParamsTippingCzk(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class UpdateParamsTippingDkk(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class UpdateParamsTippingEur(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class UpdateParamsTippingGbp(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class UpdateParamsTippingHkd(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class UpdateParamsTippingMyr(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class UpdateParamsTippingNok(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class UpdateParamsTippingNzd(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class UpdateParamsTippingSek(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class UpdateParamsTippingSgd(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class UpdateParamsTippingUsd(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class UpdateParamsVerifoneP400(TypedDict):
        splashscreen: NotRequired["Literal['']|str"]
        """
        A File ID representing an image you would like displayed on the reader.
        """

    def delete(
        self,
        configuration: str,
        params: "ConfigurationService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> Configuration:
        """
        Deletes a Configuration object.
        """
        return cast(
            Configuration,
            self._request(
                "delete",
                "/v1/terminal/configurations/{configuration}".format(
                    configuration=sanitize_id(configuration),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def delete_async(
        self,
        configuration: str,
        params: "ConfigurationService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> Configuration:
        """
        Deletes a Configuration object.
        """
        return cast(
            Configuration,
            await self._request_async(
                "delete",
                "/v1/terminal/configurations/{configuration}".format(
                    configuration=sanitize_id(configuration),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        configuration: str,
        params: "ConfigurationService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Configuration:
        """
        Retrieves a Configuration object.
        """
        return cast(
            Configuration,
            self._request(
                "get",
                "/v1/terminal/configurations/{configuration}".format(
                    configuration=sanitize_id(configuration),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        configuration: str,
        params: "ConfigurationService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Configuration:
        """
        Retrieves a Configuration object.
        """
        return cast(
            Configuration,
            await self._request_async(
                "get",
                "/v1/terminal/configurations/{configuration}".format(
                    configuration=sanitize_id(configuration),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        configuration: str,
        params: "ConfigurationService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Configuration:
        """
        Updates a new Configuration object.
        """
        return cast(
            Configuration,
            self._request(
                "post",
                "/v1/terminal/configurations/{configuration}".format(
                    configuration=sanitize_id(configuration),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        configuration: str,
        params: "ConfigurationService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Configuration:
        """
        Updates a new Configuration object.
        """
        return cast(
            Configuration,
            await self._request_async(
                "post",
                "/v1/terminal/configurations/{configuration}".format(
                    configuration=sanitize_id(configuration),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def list(
        self,
        params: "ConfigurationService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Configuration]:
        """
        Returns a list of Configuration objects.
        """
        return cast(
            ListObject[Configuration],
            self._request(
                "get",
                "/v1/terminal/configurations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "ConfigurationService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Configuration]:
        """
        Returns a list of Configuration objects.
        """
        return cast(
            ListObject[Configuration],
            await self._request_async(
                "get",
                "/v1/terminal/configurations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "ConfigurationService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Configuration:
        """
        Creates a new Configuration object.
        """
        return cast(
            Configuration,
            self._request(
                "post",
                "/v1/terminal/configurations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "ConfigurationService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Configuration:
        """
        Creates a new Configuration object.
        """
        return cast(
            Configuration,
            await self._request_async(
                "post",
                "/v1/terminal/configurations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
