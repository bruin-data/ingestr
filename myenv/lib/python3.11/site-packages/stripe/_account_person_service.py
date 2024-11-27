# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._person import Person
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class AccountPersonService(StripeService):
    class CreateParams(TypedDict):
        additional_tos_acceptances: NotRequired[
            "AccountPersonService.CreateParamsAdditionalTosAcceptances"
        ]
        """
        Details on the legal guardian's acceptance of the required Stripe agreements.
        """
        address: NotRequired["AccountPersonService.CreateParamsAddress"]
        """
        The person's address.
        """
        address_kana: NotRequired[
            "AccountPersonService.CreateParamsAddressKana"
        ]
        """
        The Kana variation of the person's address (Japan only).
        """
        address_kanji: NotRequired[
            "AccountPersonService.CreateParamsAddressKanji"
        ]
        """
        The Kanji variation of the person's address (Japan only).
        """
        dob: NotRequired["Literal['']|AccountPersonService.CreateParamsDob"]
        """
        The person's date of birth.
        """
        documents: NotRequired["AccountPersonService.CreateParamsDocuments"]
        """
        Documents that may be submitted to satisfy various informational requests.
        """
        email: NotRequired[str]
        """
        The person's email address.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        first_name: NotRequired[str]
        """
        The person's first name.
        """
        first_name_kana: NotRequired[str]
        """
        The Kana variation of the person's first name (Japan only).
        """
        first_name_kanji: NotRequired[str]
        """
        The Kanji variation of the person's first name (Japan only).
        """
        full_name_aliases: NotRequired["Literal['']|List[str]"]
        """
        A list of alternate names or aliases that the person is known by.
        """
        gender: NotRequired[str]
        """
        The person's gender (International regulations require either "male" or "female").
        """
        id_number: NotRequired[str]
        """
        The person's ID number, as appropriate for their country. For example, a social security number in the U.S., social insurance number in Canada, etc. Instead of the number itself, you can also provide a [PII token provided by Stripe.js](https://docs.stripe.com/js/tokens/create_token?type=pii).
        """
        id_number_secondary: NotRequired[str]
        """
        The person's secondary ID number, as appropriate for their country, will be used for enhanced verification checks. In Thailand, this would be the laser code found on the back of an ID card. Instead of the number itself, you can also provide a [PII token provided by Stripe.js](https://docs.stripe.com/js/tokens/create_token?type=pii).
        """
        last_name: NotRequired[str]
        """
        The person's last name.
        """
        last_name_kana: NotRequired[str]
        """
        The Kana variation of the person's last name (Japan only).
        """
        last_name_kanji: NotRequired[str]
        """
        The Kanji variation of the person's last name (Japan only).
        """
        maiden_name: NotRequired[str]
        """
        The person's maiden name.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        nationality: NotRequired[str]
        """
        The country where the person is a national. Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)), or "XX" if unavailable.
        """
        person_token: NotRequired[str]
        """
        A [person token](https://docs.stripe.com/connect/account-tokens), used to securely provide details to the person.
        """
        phone: NotRequired[str]
        """
        The person's phone number.
        """
        political_exposure: NotRequired[str]
        """
        Indicates if the person or any of their representatives, family members, or other closely related persons, declares that they hold or have held an important public job or function, in any jurisdiction.
        """
        registered_address: NotRequired[
            "AccountPersonService.CreateParamsRegisteredAddress"
        ]
        """
        The person's registered address.
        """
        relationship: NotRequired[
            "AccountPersonService.CreateParamsRelationship"
        ]
        """
        The relationship that this person has with the account's legal entity.
        """
        ssn_last_4: NotRequired[str]
        """
        The last four digits of the person's Social Security number (U.S. only).
        """
        verification: NotRequired[
            "AccountPersonService.CreateParamsVerification"
        ]
        """
        The person's verification status.
        """

    class CreateParamsAdditionalTosAcceptances(TypedDict):
        account: NotRequired[
            "AccountPersonService.CreateParamsAdditionalTosAcceptancesAccount"
        ]
        """
        Details on the legal guardian's acceptance of the main Stripe service agreement.
        """

    class CreateParamsAdditionalTosAcceptancesAccount(TypedDict):
        date: NotRequired[int]
        """
        The Unix timestamp marking when the account representative accepted the service agreement.
        """
        ip: NotRequired[str]
        """
        The IP address from which the account representative accepted the service agreement.
        """
        user_agent: NotRequired["Literal['']|str"]
        """
        The user agent of the browser from which the account representative accepted the service agreement.
        """

    class CreateParamsAddress(TypedDict):
        city: NotRequired[str]
        """
        City, district, suburb, town, or village.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Address line 1 (e.g., street, PO Box, or company name).
        """
        line2: NotRequired[str]
        """
        Address line 2 (e.g., apartment, suite, unit, or building).
        """
        postal_code: NotRequired[str]
        """
        ZIP or postal code.
        """
        state: NotRequired[str]
        """
        State, county, province, or region.
        """

    class CreateParamsAddressKana(TypedDict):
        city: NotRequired[str]
        """
        City or ward.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Block or building number.
        """
        line2: NotRequired[str]
        """
        Building details.
        """
        postal_code: NotRequired[str]
        """
        Postal code.
        """
        state: NotRequired[str]
        """
        Prefecture.
        """
        town: NotRequired[str]
        """
        Town or cho-me.
        """

    class CreateParamsAddressKanji(TypedDict):
        city: NotRequired[str]
        """
        City or ward.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Block or building number.
        """
        line2: NotRequired[str]
        """
        Building details.
        """
        postal_code: NotRequired[str]
        """
        Postal code.
        """
        state: NotRequired[str]
        """
        Prefecture.
        """
        town: NotRequired[str]
        """
        Town or cho-me.
        """

    class CreateParamsDob(TypedDict):
        day: int
        """
        The day of birth, between 1 and 31.
        """
        month: int
        """
        The month of birth, between 1 and 12.
        """
        year: int
        """
        The four-digit year of birth.
        """

    class CreateParamsDocuments(TypedDict):
        company_authorization: NotRequired[
            "AccountPersonService.CreateParamsDocumentsCompanyAuthorization"
        ]
        """
        One or more documents that demonstrate proof that this person is authorized to represent the company.
        """
        passport: NotRequired[
            "AccountPersonService.CreateParamsDocumentsPassport"
        ]
        """
        One or more documents showing the person's passport page with photo and personal data.
        """
        visa: NotRequired["AccountPersonService.CreateParamsDocumentsVisa"]
        """
        One or more documents showing the person's visa required for living in the country where they are residing.
        """

    class CreateParamsDocumentsCompanyAuthorization(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class CreateParamsDocumentsPassport(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class CreateParamsDocumentsVisa(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class CreateParamsRegisteredAddress(TypedDict):
        city: NotRequired[str]
        """
        City, district, suburb, town, or village.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Address line 1 (e.g., street, PO Box, or company name).
        """
        line2: NotRequired[str]
        """
        Address line 2 (e.g., apartment, suite, unit, or building).
        """
        postal_code: NotRequired[str]
        """
        ZIP or postal code.
        """
        state: NotRequired[str]
        """
        State, county, province, or region.
        """

    class CreateParamsRelationship(TypedDict):
        director: NotRequired[bool]
        """
        Whether the person is a director of the account's legal entity. Directors are typically members of the governing board of the company, or responsible for ensuring the company meets its regulatory obligations.
        """
        executive: NotRequired[bool]
        """
        Whether the person has significant responsibility to control, manage, or direct the organization.
        """
        legal_guardian: NotRequired[bool]
        """
        Whether the person is the legal guardian of the account's representative.
        """
        owner: NotRequired[bool]
        """
        Whether the person is an owner of the account's legal entity.
        """
        percent_ownership: NotRequired["Literal['']|float"]
        """
        The percent owned by the person of the account's legal entity.
        """
        representative: NotRequired[bool]
        """
        Whether the person is authorized as the primary representative of the account. This is the person nominated by the business to provide information about themselves, and general information about the account. There can only be one representative at any given time. At the time the account is created, this person should be set to the person responsible for opening the account.
        """
        title: NotRequired[str]
        """
        The person's title (e.g., CEO, Support Engineer).
        """

    class CreateParamsVerification(TypedDict):
        additional_document: NotRequired[
            "AccountPersonService.CreateParamsVerificationAdditionalDocument"
        ]
        """
        A document showing address, either a passport, local ID card, or utility bill from a well-known utility company.
        """
        document: NotRequired[
            "AccountPersonService.CreateParamsVerificationDocument"
        ]
        """
        An identifying document, either a passport or local ID card.
        """

    class CreateParamsVerificationAdditionalDocument(TypedDict):
        back: NotRequired[str]
        """
        The back of an ID returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `identity_document`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """
        front: NotRequired[str]
        """
        The front of an ID returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `identity_document`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """

    class CreateParamsVerificationDocument(TypedDict):
        back: NotRequired[str]
        """
        The back of an ID returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `identity_document`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """
        front: NotRequired[str]
        """
        The front of an ID returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `identity_document`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
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
        relationship: NotRequired[
            "AccountPersonService.ListParamsRelationship"
        ]
        """
        Filters on the list of people returned based on the person's relationship to the account's company.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """

    class ListParamsRelationship(TypedDict):
        director: NotRequired[bool]
        """
        A filter on the list of people returned based on whether these people are directors of the account's company.
        """
        executive: NotRequired[bool]
        """
        A filter on the list of people returned based on whether these people are executives of the account's company.
        """
        legal_guardian: NotRequired[bool]
        """
        A filter on the list of people returned based on whether these people are legal guardians of the account's representative.
        """
        owner: NotRequired[bool]
        """
        A filter on the list of people returned based on whether these people are owners of the account's company.
        """
        representative: NotRequired[bool]
        """
        A filter on the list of people returned based on whether these people are the representative of the account's company.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        additional_tos_acceptances: NotRequired[
            "AccountPersonService.UpdateParamsAdditionalTosAcceptances"
        ]
        """
        Details on the legal guardian's acceptance of the required Stripe agreements.
        """
        address: NotRequired["AccountPersonService.UpdateParamsAddress"]
        """
        The person's address.
        """
        address_kana: NotRequired[
            "AccountPersonService.UpdateParamsAddressKana"
        ]
        """
        The Kana variation of the person's address (Japan only).
        """
        address_kanji: NotRequired[
            "AccountPersonService.UpdateParamsAddressKanji"
        ]
        """
        The Kanji variation of the person's address (Japan only).
        """
        dob: NotRequired["Literal['']|AccountPersonService.UpdateParamsDob"]
        """
        The person's date of birth.
        """
        documents: NotRequired["AccountPersonService.UpdateParamsDocuments"]
        """
        Documents that may be submitted to satisfy various informational requests.
        """
        email: NotRequired[str]
        """
        The person's email address.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        first_name: NotRequired[str]
        """
        The person's first name.
        """
        first_name_kana: NotRequired[str]
        """
        The Kana variation of the person's first name (Japan only).
        """
        first_name_kanji: NotRequired[str]
        """
        The Kanji variation of the person's first name (Japan only).
        """
        full_name_aliases: NotRequired["Literal['']|List[str]"]
        """
        A list of alternate names or aliases that the person is known by.
        """
        gender: NotRequired[str]
        """
        The person's gender (International regulations require either "male" or "female").
        """
        id_number: NotRequired[str]
        """
        The person's ID number, as appropriate for their country. For example, a social security number in the U.S., social insurance number in Canada, etc. Instead of the number itself, you can also provide a [PII token provided by Stripe.js](https://docs.stripe.com/js/tokens/create_token?type=pii).
        """
        id_number_secondary: NotRequired[str]
        """
        The person's secondary ID number, as appropriate for their country, will be used for enhanced verification checks. In Thailand, this would be the laser code found on the back of an ID card. Instead of the number itself, you can also provide a [PII token provided by Stripe.js](https://docs.stripe.com/js/tokens/create_token?type=pii).
        """
        last_name: NotRequired[str]
        """
        The person's last name.
        """
        last_name_kana: NotRequired[str]
        """
        The Kana variation of the person's last name (Japan only).
        """
        last_name_kanji: NotRequired[str]
        """
        The Kanji variation of the person's last name (Japan only).
        """
        maiden_name: NotRequired[str]
        """
        The person's maiden name.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        nationality: NotRequired[str]
        """
        The country where the person is a national. Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)), or "XX" if unavailable.
        """
        person_token: NotRequired[str]
        """
        A [person token](https://docs.stripe.com/connect/account-tokens), used to securely provide details to the person.
        """
        phone: NotRequired[str]
        """
        The person's phone number.
        """
        political_exposure: NotRequired[str]
        """
        Indicates if the person or any of their representatives, family members, or other closely related persons, declares that they hold or have held an important public job or function, in any jurisdiction.
        """
        registered_address: NotRequired[
            "AccountPersonService.UpdateParamsRegisteredAddress"
        ]
        """
        The person's registered address.
        """
        relationship: NotRequired[
            "AccountPersonService.UpdateParamsRelationship"
        ]
        """
        The relationship that this person has with the account's legal entity.
        """
        ssn_last_4: NotRequired[str]
        """
        The last four digits of the person's Social Security number (U.S. only).
        """
        verification: NotRequired[
            "AccountPersonService.UpdateParamsVerification"
        ]
        """
        The person's verification status.
        """

    class UpdateParamsAdditionalTosAcceptances(TypedDict):
        account: NotRequired[
            "AccountPersonService.UpdateParamsAdditionalTosAcceptancesAccount"
        ]
        """
        Details on the legal guardian's acceptance of the main Stripe service agreement.
        """

    class UpdateParamsAdditionalTosAcceptancesAccount(TypedDict):
        date: NotRequired[int]
        """
        The Unix timestamp marking when the account representative accepted the service agreement.
        """
        ip: NotRequired[str]
        """
        The IP address from which the account representative accepted the service agreement.
        """
        user_agent: NotRequired["Literal['']|str"]
        """
        The user agent of the browser from which the account representative accepted the service agreement.
        """

    class UpdateParamsAddress(TypedDict):
        city: NotRequired[str]
        """
        City, district, suburb, town, or village.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Address line 1 (e.g., street, PO Box, or company name).
        """
        line2: NotRequired[str]
        """
        Address line 2 (e.g., apartment, suite, unit, or building).
        """
        postal_code: NotRequired[str]
        """
        ZIP or postal code.
        """
        state: NotRequired[str]
        """
        State, county, province, or region.
        """

    class UpdateParamsAddressKana(TypedDict):
        city: NotRequired[str]
        """
        City or ward.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Block or building number.
        """
        line2: NotRequired[str]
        """
        Building details.
        """
        postal_code: NotRequired[str]
        """
        Postal code.
        """
        state: NotRequired[str]
        """
        Prefecture.
        """
        town: NotRequired[str]
        """
        Town or cho-me.
        """

    class UpdateParamsAddressKanji(TypedDict):
        city: NotRequired[str]
        """
        City or ward.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Block or building number.
        """
        line2: NotRequired[str]
        """
        Building details.
        """
        postal_code: NotRequired[str]
        """
        Postal code.
        """
        state: NotRequired[str]
        """
        Prefecture.
        """
        town: NotRequired[str]
        """
        Town or cho-me.
        """

    class UpdateParamsDob(TypedDict):
        day: int
        """
        The day of birth, between 1 and 31.
        """
        month: int
        """
        The month of birth, between 1 and 12.
        """
        year: int
        """
        The four-digit year of birth.
        """

    class UpdateParamsDocuments(TypedDict):
        company_authorization: NotRequired[
            "AccountPersonService.UpdateParamsDocumentsCompanyAuthorization"
        ]
        """
        One or more documents that demonstrate proof that this person is authorized to represent the company.
        """
        passport: NotRequired[
            "AccountPersonService.UpdateParamsDocumentsPassport"
        ]
        """
        One or more documents showing the person's passport page with photo and personal data.
        """
        visa: NotRequired["AccountPersonService.UpdateParamsDocumentsVisa"]
        """
        One or more documents showing the person's visa required for living in the country where they are residing.
        """

    class UpdateParamsDocumentsCompanyAuthorization(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class UpdateParamsDocumentsPassport(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class UpdateParamsDocumentsVisa(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class UpdateParamsRegisteredAddress(TypedDict):
        city: NotRequired[str]
        """
        City, district, suburb, town, or village.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Address line 1 (e.g., street, PO Box, or company name).
        """
        line2: NotRequired[str]
        """
        Address line 2 (e.g., apartment, suite, unit, or building).
        """
        postal_code: NotRequired[str]
        """
        ZIP or postal code.
        """
        state: NotRequired[str]
        """
        State, county, province, or region.
        """

    class UpdateParamsRelationship(TypedDict):
        director: NotRequired[bool]
        """
        Whether the person is a director of the account's legal entity. Directors are typically members of the governing board of the company, or responsible for ensuring the company meets its regulatory obligations.
        """
        executive: NotRequired[bool]
        """
        Whether the person has significant responsibility to control, manage, or direct the organization.
        """
        legal_guardian: NotRequired[bool]
        """
        Whether the person is the legal guardian of the account's representative.
        """
        owner: NotRequired[bool]
        """
        Whether the person is an owner of the account's legal entity.
        """
        percent_ownership: NotRequired["Literal['']|float"]
        """
        The percent owned by the person of the account's legal entity.
        """
        representative: NotRequired[bool]
        """
        Whether the person is authorized as the primary representative of the account. This is the person nominated by the business to provide information about themselves, and general information about the account. There can only be one representative at any given time. At the time the account is created, this person should be set to the person responsible for opening the account.
        """
        title: NotRequired[str]
        """
        The person's title (e.g., CEO, Support Engineer).
        """

    class UpdateParamsVerification(TypedDict):
        additional_document: NotRequired[
            "AccountPersonService.UpdateParamsVerificationAdditionalDocument"
        ]
        """
        A document showing address, either a passport, local ID card, or utility bill from a well-known utility company.
        """
        document: NotRequired[
            "AccountPersonService.UpdateParamsVerificationDocument"
        ]
        """
        An identifying document, either a passport or local ID card.
        """

    class UpdateParamsVerificationAdditionalDocument(TypedDict):
        back: NotRequired[str]
        """
        The back of an ID returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `identity_document`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """
        front: NotRequired[str]
        """
        The front of an ID returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `identity_document`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """

    class UpdateParamsVerificationDocument(TypedDict):
        back: NotRequired[str]
        """
        The back of an ID returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `identity_document`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """
        front: NotRequired[str]
        """
        The front of an ID returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `identity_document`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """

    def delete(
        self,
        account: str,
        person: str,
        params: "AccountPersonService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> Person:
        """
        Deletes an existing person's relationship to the account's legal entity. Any person with a relationship for an account can be deleted through the API, except if the person is the account_opener. If your integration is using the executive parameter, you cannot delete the only verified executive on file.
        """
        return cast(
            Person,
            self._request(
                "delete",
                "/v1/accounts/{account}/persons/{person}".format(
                    account=sanitize_id(account),
                    person=sanitize_id(person),
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
        person: str,
        params: "AccountPersonService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> Person:
        """
        Deletes an existing person's relationship to the account's legal entity. Any person with a relationship for an account can be deleted through the API, except if the person is the account_opener. If your integration is using the executive parameter, you cannot delete the only verified executive on file.
        """
        return cast(
            Person,
            await self._request_async(
                "delete",
                "/v1/accounts/{account}/persons/{person}".format(
                    account=sanitize_id(account),
                    person=sanitize_id(person),
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
        person: str,
        params: "AccountPersonService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Person:
        """
        Retrieves an existing person.
        """
        return cast(
            Person,
            self._request(
                "get",
                "/v1/accounts/{account}/persons/{person}".format(
                    account=sanitize_id(account),
                    person=sanitize_id(person),
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
        person: str,
        params: "AccountPersonService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Person:
        """
        Retrieves an existing person.
        """
        return cast(
            Person,
            await self._request_async(
                "get",
                "/v1/accounts/{account}/persons/{person}".format(
                    account=sanitize_id(account),
                    person=sanitize_id(person),
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
        person: str,
        params: "AccountPersonService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Person:
        """
        Updates an existing person.
        """
        return cast(
            Person,
            self._request(
                "post",
                "/v1/accounts/{account}/persons/{person}".format(
                    account=sanitize_id(account),
                    person=sanitize_id(person),
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
        person: str,
        params: "AccountPersonService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Person:
        """
        Updates an existing person.
        """
        return cast(
            Person,
            await self._request_async(
                "post",
                "/v1/accounts/{account}/persons/{person}".format(
                    account=sanitize_id(account),
                    person=sanitize_id(person),
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
        params: "AccountPersonService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Person]:
        """
        Returns a list of people associated with the account's legal entity. The people are returned sorted by creation date, with the most recent people appearing first.
        """
        return cast(
            ListObject[Person],
            self._request(
                "get",
                "/v1/accounts/{account}/persons".format(
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
        params: "AccountPersonService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Person]:
        """
        Returns a list of people associated with the account's legal entity. The people are returned sorted by creation date, with the most recent people appearing first.
        """
        return cast(
            ListObject[Person],
            await self._request_async(
                "get",
                "/v1/accounts/{account}/persons".format(
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
        params: "AccountPersonService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Person:
        """
        Creates a new person.
        """
        return cast(
            Person,
            self._request(
                "post",
                "/v1/accounts/{account}/persons".format(
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
        params: "AccountPersonService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Person:
        """
        Creates a new person.
        """
        return cast(
            Person,
            await self._request_async(
                "post",
                "/v1/accounts/{account}/persons".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
