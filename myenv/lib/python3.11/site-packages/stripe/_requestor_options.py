# using global variables
import stripe  # noqa: IMP101
from stripe._base_address import BaseAddresses

from typing import Optional


class RequestorOptions(object):
    api_key: Optional[str]
    stripe_account: Optional[str]
    stripe_version: Optional[str]
    base_addresses: BaseAddresses
    max_network_retries: Optional[int]

    def __init__(
        self,
        api_key: Optional[str] = None,
        stripe_account: Optional[str] = None,
        stripe_version: Optional[str] = None,
        base_addresses: BaseAddresses = {},
        max_network_retries: Optional[int] = None,
    ):
        self.api_key = api_key
        self.stripe_account = stripe_account
        self.stripe_version = stripe_version
        self.base_addresses = {}

        # Base addresses can be unset (for correct merging).
        # If they are not set, then we will use default API bases defined on stripe.
        if base_addresses.get("api"):
            self.base_addresses["api"] = base_addresses.get("api")
        if base_addresses.get("connect") is not None:
            self.base_addresses["connect"] = base_addresses.get("connect")
        if base_addresses.get("files") is not None:
            self.base_addresses["files"] = base_addresses.get("files")

        self.max_network_retries = max_network_retries

    def to_dict(self):
        """
        Returns a dict representation of the object.
        """
        return {
            "api_key": self.api_key,
            "stripe_account": self.stripe_account,
            "stripe_version": self.stripe_version,
            "base_addresses": self.base_addresses,
            "max_network_retries": self.max_network_retries,
        }


class _GlobalRequestorOptions(RequestorOptions):
    def __init__(self):
        pass

    @property
    def base_addresses(self):
        return {
            "api": stripe.api_base,
            "connect": stripe.connect_api_base,
            "files": stripe.upload_api_base,
        }

    @property
    def api_key(self):
        return stripe.api_key

    @property
    def stripe_version(self):
        return stripe.api_version

    @property
    def stripe_account(self):
        return None

    @property
    def max_network_retries(self):
        return stripe.max_network_retries
