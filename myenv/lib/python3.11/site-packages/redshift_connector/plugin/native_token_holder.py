import datetime
import typing

from dateutil.tz import tzutc


class NativeTokenHolder:
    """
    Holds Redshift Native authentication credentials.
    """

    def __init__(self: "NativeTokenHolder", access_token: str, expiration: typing.Optional[str]):
        self.access_token: str = access_token
        self.expiration = expiration
        self.refresh: bool = False  # True means newly added, false means from cache

    def is_expired(self: "NativeTokenHolder") -> bool:
        """
        Returns boolean value indicating if the Redshift native authentication credentials have expired.
        """
        return self.expiration is None or typing.cast(datetime.datetime, self.expiration) < datetime.datetime.now(
            tz=tzutc()
        )
