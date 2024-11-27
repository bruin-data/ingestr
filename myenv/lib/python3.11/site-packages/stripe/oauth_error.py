# Used for global variables
import stripe  # noqa: IMP101
from stripe._error import StripeError


class OAuthError(StripeError):
    def __init__(
        self,
        code,
        description,
        http_body=None,
        http_status=None,
        json_body=None,
        headers=None,
    ):
        super(OAuthError, self).__init__(
            description, http_body, http_status, json_body, headers, code
        )

    def _construct_error_object(self):
        if self.json_body is None:
            return None

        return stripe.error_object.OAuthErrorObject._construct_from(  # pyright: ignore
            values=self.json_body,
            requestor=stripe._APIRequestor._global_instance(),
            api_mode="V1",
        )


class InvalidClientError(OAuthError):
    pass


class InvalidGrantError(OAuthError):
    pass


class InvalidRequestError(OAuthError):
    pass


class InvalidScopeError(OAuthError):
    pass


class UnsupportedGrantTypeError(OAuthError):
    pass


class UnsupportedResponseTypeError(OAuthError):
    pass
