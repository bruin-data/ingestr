from abc import abstractmethod

from redshift_connector.plugin.i_plugin import IPlugin


class INativePlugin(IPlugin):
    """
    Abstract base class for authentication plugins using Redshift Native authentication
    """

    @abstractmethod
    def get_idp_token(self: "INativePlugin") -> str:
        """
        Returns the IdP token retrieved after authenticating with the plugin.
        """
        pass  # pragma: no cover
