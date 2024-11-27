class DriverInfo:
    """
    No-op informative class containing  Amazon Redshift Python driver specifications.
    """

    @staticmethod
    def version() -> str:
        """
        The version of redshift_connector
        Returns
        -------
        The redshift_connector package version: str
        """
        from redshift_connector import __version__ as DRIVER_VERSION

        return str(DRIVER_VERSION)

    @staticmethod
    def driver_name() -> str:
        """
        The name of the Amazon Redshift Python driver, redshift_connector
        Returns
        -------
        The human readable name of the redshift_connector package: str
        """
        return "Redshift Python Driver"

    @staticmethod
    def driver_short_name() -> str:
        """
        The shortened name of the Amazon Redshift Python driver, redshift_connector
        Returns
        -------
        The shortened human readable name of the Amazon Redshift Python driver: str
        """
        return "RsPython"

    @staticmethod
    def driver_full_name() -> str:
        """
        The fully qualified name of the Amazon Redshift Python driver, redshift_connector
        Returns
        -------
        The fully qualified name of the Amazon Redshift Python driver: str
        """
        return "{driver_name} {driver_version}".format(
            driver_name=DriverInfo.driver_name(), driver_version=DriverInfo.version()
        )
