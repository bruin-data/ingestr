# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._stripe_object import StripeObject
from typing import ClassVar, List, Optional
from typing_extensions import Literal


class FundingInstructions(StripeObject):
    """
    Each customer has a [`balance`](https://stripe.com/docs/api/customers/object#customer_object-balance) that is
    automatically applied to future invoices and payments using the `customer_balance` payment method.
    Customers can fund this balance by initiating a bank transfer to any account in the
    `financial_addresses` field.
    Related guide: [Customer balance funding instructions](https://stripe.com/docs/payments/customer-balance/funding-instructions)
    """

    OBJECT_NAME: ClassVar[Literal["funding_instructions"]] = (
        "funding_instructions"
    )

    class BankTransfer(StripeObject):
        class FinancialAddress(StripeObject):
            class Aba(StripeObject):
                account_number: str
                """
                The ABA account number
                """
                bank_name: str
                """
                The bank name
                """
                routing_number: str
                """
                The ABA routing number
                """

            class Iban(StripeObject):
                account_holder_name: str
                """
                The name of the person or business that owns the bank account
                """
                bic: str
                """
                The BIC/SWIFT code of the account.
                """
                country: str
                """
                Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
                """
                iban: str
                """
                The IBAN of the account.
                """

            class SortCode(StripeObject):
                account_holder_name: str
                """
                The name of the person or business that owns the bank account
                """
                account_number: str
                """
                The account number
                """
                sort_code: str
                """
                The six-digit sort code
                """

            class Spei(StripeObject):
                bank_code: str
                """
                The three-digit bank code
                """
                bank_name: str
                """
                The short banking institution name
                """
                clabe: str
                """
                The CLABE number
                """

            class Swift(StripeObject):
                account_number: str
                """
                The account number
                """
                bank_name: str
                """
                The bank name
                """
                swift_code: str
                """
                The SWIFT code
                """

            class Zengin(StripeObject):
                account_holder_name: Optional[str]
                """
                The account holder name
                """
                account_number: Optional[str]
                """
                The account number
                """
                account_type: Optional[str]
                """
                The bank account type. In Japan, this can only be `futsu` or `toza`.
                """
                bank_code: Optional[str]
                """
                The bank code of the account
                """
                bank_name: Optional[str]
                """
                The bank name of the account
                """
                branch_code: Optional[str]
                """
                The branch code of the account
                """
                branch_name: Optional[str]
                """
                The branch name of the account
                """

            aba: Optional[Aba]
            """
            ABA Records contain U.S. bank account details per the ABA format.
            """
            iban: Optional[Iban]
            """
            Iban Records contain E.U. bank account details per the SEPA format.
            """
            sort_code: Optional[SortCode]
            """
            Sort Code Records contain U.K. bank account details per the sort code format.
            """
            spei: Optional[Spei]
            """
            SPEI Records contain Mexico bank account details per the SPEI format.
            """
            supported_networks: Optional[
                List[
                    Literal[
                        "ach",
                        "bacs",
                        "domestic_wire_us",
                        "fps",
                        "sepa",
                        "spei",
                        "swift",
                        "zengin",
                    ]
                ]
            ]
            """
            The payment networks supported by this FinancialAddress
            """
            swift: Optional[Swift]
            """
            SWIFT Records contain U.S. bank account details per the SWIFT format.
            """
            type: Literal[
                "aba", "iban", "sort_code", "spei", "swift", "zengin"
            ]
            """
            The type of financial address
            """
            zengin: Optional[Zengin]
            """
            Zengin Records contain Japan bank account details per the Zengin format.
            """
            _inner_class_types = {
                "aba": Aba,
                "iban": Iban,
                "sort_code": SortCode,
                "spei": Spei,
                "swift": Swift,
                "zengin": Zengin,
            }

        country: str
        """
        The country of the bank account to fund
        """
        financial_addresses: List[FinancialAddress]
        """
        A list of financial addresses that can be used to fund a particular balance
        """
        type: Literal["eu_bank_transfer", "jp_bank_transfer"]
        """
        The bank_transfer type
        """
        _inner_class_types = {"financial_addresses": FinancialAddress}

    bank_transfer: BankTransfer
    currency: str
    """
    Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    funding_type: Literal["bank_transfer"]
    """
    The `funding_type` of the returned instructions
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["funding_instructions"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    _inner_class_types = {"bank_transfer": BankTransfer}
