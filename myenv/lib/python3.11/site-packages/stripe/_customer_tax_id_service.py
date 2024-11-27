# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._tax_id import TaxId
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class CustomerTaxIdService(StripeService):
    class CreateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        type: Literal[
            "ad_nrt",
            "ae_trn",
            "ar_cuit",
            "au_abn",
            "au_arn",
            "bg_uic",
            "bh_vat",
            "bo_tin",
            "br_cnpj",
            "br_cpf",
            "ca_bn",
            "ca_gst_hst",
            "ca_pst_bc",
            "ca_pst_mb",
            "ca_pst_sk",
            "ca_qst",
            "ch_uid",
            "ch_vat",
            "cl_tin",
            "cn_tin",
            "co_nit",
            "cr_tin",
            "de_stn",
            "do_rcn",
            "ec_ruc",
            "eg_tin",
            "es_cif",
            "eu_oss_vat",
            "eu_vat",
            "gb_vat",
            "ge_vat",
            "hk_br",
            "hu_tin",
            "id_npwp",
            "il_vat",
            "in_gst",
            "is_vat",
            "jp_cn",
            "jp_rn",
            "jp_trn",
            "ke_pin",
            "kr_brn",
            "kz_bin",
            "li_uid",
            "mx_rfc",
            "my_frp",
            "my_itn",
            "my_sst",
            "ng_tin",
            "no_vat",
            "no_voec",
            "nz_gst",
            "om_vat",
            "pe_ruc",
            "ph_tin",
            "ro_tin",
            "rs_pib",
            "ru_inn",
            "ru_kpp",
            "sa_vat",
            "sg_gst",
            "sg_uen",
            "si_tin",
            "sv_nit",
            "th_vat",
            "tr_tin",
            "tw_vat",
            "ua_vat",
            "us_ein",
            "uy_ruc",
            "ve_rif",
            "vn_tin",
            "za_vat",
        ]
        """
        Type of the tax ID, one of `ad_nrt`, `ae_trn`, `ar_cuit`, `au_abn`, `au_arn`, `bg_uic`, `bh_vat`, `bo_tin`, `br_cnpj`, `br_cpf`, `ca_bn`, `ca_gst_hst`, `ca_pst_bc`, `ca_pst_mb`, `ca_pst_sk`, `ca_qst`, `ch_uid`, `ch_vat`, `cl_tin`, `cn_tin`, `co_nit`, `cr_tin`, `de_stn`, `do_rcn`, `ec_ruc`, `eg_tin`, `es_cif`, `eu_oss_vat`, `eu_vat`, `gb_vat`, `ge_vat`, `hk_br`, `hu_tin`, `id_npwp`, `il_vat`, `in_gst`, `is_vat`, `jp_cn`, `jp_rn`, `jp_trn`, `ke_pin`, `kr_brn`, `kz_bin`, `li_uid`, `mx_rfc`, `my_frp`, `my_itn`, `my_sst`, `ng_tin`, `no_vat`, `no_voec`, `nz_gst`, `om_vat`, `pe_ruc`, `ph_tin`, `ro_tin`, `rs_pib`, `ru_inn`, `ru_kpp`, `sa_vat`, `sg_gst`, `sg_uen`, `si_tin`, `sv_nit`, `th_vat`, `tr_tin`, `tw_vat`, `ua_vat`, `us_ein`, `uy_ruc`, `ve_rif`, `vn_tin`, or `za_vat`
        """
        value: str
        """
        Value of the tax ID.
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

    def delete(
        self,
        customer: str,
        id: str,
        params: "CustomerTaxIdService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> TaxId:
        """
        Deletes an existing tax_id object.
        """
        return cast(
            TaxId,
            self._request(
                "delete",
                "/v1/customers/{customer}/tax_ids/{id}".format(
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
        params: "CustomerTaxIdService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> TaxId:
        """
        Deletes an existing tax_id object.
        """
        return cast(
            TaxId,
            await self._request_async(
                "delete",
                "/v1/customers/{customer}/tax_ids/{id}".format(
                    customer=sanitize_id(customer),
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
        customer: str,
        id: str,
        params: "CustomerTaxIdService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> TaxId:
        """
        Retrieves the tax_id object with the given identifier.
        """
        return cast(
            TaxId,
            self._request(
                "get",
                "/v1/customers/{customer}/tax_ids/{id}".format(
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
        params: "CustomerTaxIdService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> TaxId:
        """
        Retrieves the tax_id object with the given identifier.
        """
        return cast(
            TaxId,
            await self._request_async(
                "get",
                "/v1/customers/{customer}/tax_ids/{id}".format(
                    customer=sanitize_id(customer),
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
        customer: str,
        params: "CustomerTaxIdService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[TaxId]:
        """
        Returns a list of tax IDs for a customer.
        """
        return cast(
            ListObject[TaxId],
            self._request(
                "get",
                "/v1/customers/{customer}/tax_ids".format(
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
        params: "CustomerTaxIdService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[TaxId]:
        """
        Returns a list of tax IDs for a customer.
        """
        return cast(
            ListObject[TaxId],
            await self._request_async(
                "get",
                "/v1/customers/{customer}/tax_ids".format(
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
        params: "CustomerTaxIdService.CreateParams",
        options: RequestOptions = {},
    ) -> TaxId:
        """
        Creates a new tax_id object for a customer.
        """
        return cast(
            TaxId,
            self._request(
                "post",
                "/v1/customers/{customer}/tax_ids".format(
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
        params: "CustomerTaxIdService.CreateParams",
        options: RequestOptions = {},
    ) -> TaxId:
        """
        Creates a new tax_id object for a customer.
        """
        return cast(
            TaxId,
            await self._request_async(
                "post",
                "/v1/customers/{customer}/tax_ids".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
