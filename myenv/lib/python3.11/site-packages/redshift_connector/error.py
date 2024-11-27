class Warning(Exception):
    """Generic exception raised for important database warnings like data
    truncations.  This exception is not currently used.

    This exception is part of the `DBAPI 2.0 specification
    <http://www.python.org/dev/peps/pep-0249/>`_.
    """

    pass


class Error(Exception):
    """Generic exception that is the base exception of all other error
    exceptions.

    This exception is part of the `DBAPI 2.0 specification
    <http://www.python.org/dev/peps/pep-0249/>`_.
    """

    pass


class InterfaceError(Error):
    """Generic exception raised for errors that are related to the database
    interface rather than the database itself.  For example, if the interface
    attempts to use an SSL connection but the server refuses, an InterfaceError
    will be raised.

    This exception is part of the `DBAPI 2.0 specification
    <http://www.python.org/dev/peps/pep-0249/>`_.
    """

    pass


class DatabaseError(Error):
    """Generic exception raised for errors that are related to the database.
    This exception is currently never raised.

    This exception is part of the `DBAPI 2.0 specification
    <http://www.python.org/dev/peps/pep-0249/>`_.
    """

    pass


class DataError(DatabaseError):
    """Generic exception raised for errors that are due to problems with the
    processed data.  This exception is not currently raised.

    This exception is part of the `DBAPI 2.0 specification
    <http://www.python.org/dev/peps/pep-0249/>`_.
    """

    pass


class OperationalError(DatabaseError):
    """
    Generic exception raised for errors that are related to the database's
    operation and not necessarily under the control of the programmer. This
    exception is currently never raised.

    This exception is part of the `DBAPI 2.0 specification
    <http://www.python.org/dev/peps/pep-0249/>`_.
    """

    pass


class IntegrityError(DatabaseError):
    """
    Generic exception raised when the relational integrity of the database is
    affected.  This exception is not currently raised.

    This exception is part of the `DBAPI 2.0 specification
    <http://www.python.org/dev/peps/pep-0249/>`_.
    """

    pass


class InternalError(DatabaseError):
    """Generic exception raised when the database encounters an internal error.
    This is currently only raised when unexpected state occurs in the
    interface itself, and is typically the result of a interface bug.

    This exception is part of the `DBAPI 2.0 specification
    <http://www.python.org/dev/peps/pep-0249/>`_.
    """

    pass


class ProgrammingError(DatabaseError):
    """Generic exception raised for programming errors.  For example, this
    exception is raised if more parameter fields are in a query string than
    there are available parameters.

    This exception is part of the `DBAPI 2.0 specification
    <http://www.python.org/dev/peps/pep-0249/>`_.
    """

    pass


class NotSupportedError(DatabaseError):
    """Generic exception raised in case a method or database API was used which
    is not supported by the database.

    This exception is part of the `DBAPI 2.0 specification
    <http://www.python.org/dev/peps/pep-0249/>`_.
    """

    pass


class ArrayContentNotSupportedError(NotSupportedError):
    """
    Raised when attempting to transmit an array where the base type is not
    supported for binary data transfer by the interface.
    """

    pass


class ArrayContentNotHomogenousError(ProgrammingError):
    """
    Raised when attempting to transmit an array that doesn't contain only a
    single type of object.
    """

    pass


class ArrayDimensionsNotConsistentError(ProgrammingError):
    """
    Raised when attempting to transmit an array that has inconsistent
    multi-dimension sizes.
    """

    pass


MISSING_MODULE_ERROR_MSG: str = (
    "redshift_connector requires {module} support for this functionality. "
    "Please install redshift_connector[full] for {module} support"
)
