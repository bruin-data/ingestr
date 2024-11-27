import datetime

from databricks.sql.exc import *

# PEP 249 module globals
apilevel = "2.0"
threadsafety = 1  # Threads may share the module, but not connections.
paramstyle = "pyformat"  # Python extended format codes, e.g. ...WHERE name=%(name)s


class DBAPITypeObject(object):
    def __init__(self, *values):
        self.values = values

    def __eq__(self, other):
        return other in self.values

    def __repr__(self):
        return "DBAPITypeObject({})".format(self.values)


STRING = DBAPITypeObject("string")
BINARY = DBAPITypeObject("binary")
NUMBER = DBAPITypeObject(
    "boolean", "tinyint", "smallint", "int", "bigint", "float", "double", "decimal"
)
DATETIME = DBAPITypeObject("timestamp")
DATE = DBAPITypeObject("date")
ROWID = DBAPITypeObject()

__version__ = "2.9.3"
USER_AGENT_NAME = "PyDatabricksSqlConnector"

# These two functions are pyhive legacy
Date = datetime.date
Timestamp = datetime.datetime


def DateFromTicks(ticks):
    return Date(*time.localtime(ticks)[:3])


def TimestampFromTicks(ticks):
    return Timestamp(*time.localtime(ticks)[:6])


def connect(server_hostname, http_path, access_token=None, **kwargs):
    from .client import Connection

    return Connection(server_hostname, http_path, access_token, **kwargs)
