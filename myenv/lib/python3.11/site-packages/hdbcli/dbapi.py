import time
import datetime
import decimal
import sys
import pyhdbcli

if sys.version_info >= (3,):
    long = int
    buffer = memoryview
    unicode = str

#
# globals
#
apilevel = '2.0'
threadsafety = 1
paramstyle = ('qmark', 'named')

Connection = pyhdbcli.Connection
LOB = pyhdbcli.LOB
ResultRow = pyhdbcli.ResultRow
connect = Connection
Cursor = pyhdbcli.Cursor

#
# exceptions
#
from pyhdbcli import Warning
from pyhdbcli import Error
def __errorinit(self, *args):
    super(Error, self).__init__(*args)
    argc = len(args)
    if argc == 1:
        if isinstance(args[0], Error):
            self.errorcode = args[0].errorcode
            self.errortext = args[0].errortext
        elif isinstance(args[0], (str, unicode)):
            self.errorcode = 0
            self.errortext = args[0]
    elif argc >= 2 and isinstance(args[0], (int, long)) and isinstance(args[1], (str, unicode)):
        self.errorcode = args[0]
        self.errortext = args[1]
Error.__init__ = __errorinit
from pyhdbcli import DatabaseError
from pyhdbcli import OperationalError
from pyhdbcli import ProgrammingError
from pyhdbcli import IntegrityError
from pyhdbcli import InterfaceError
from pyhdbcli import InternalError
from pyhdbcli import DataError
from pyhdbcli import NotSupportedError
from pyhdbcli import ExecuteManyError
from pyhdbcli import ExecuteManyErrorEntry

#
# input conversions
#

def Date(year, month, day):
    return datetime.date(year, month, day)

def Time(hour, minute, second, millisecond = 0):
    return datetime.time(hour, minute, second, millisecond * 1000)

def Timestamp(year, month, day, hour, minute, second, millisecond = 0):
    return datetime.datetime(year, month, day, hour, minute, second, millisecond * 1000)

def DateFromTicks(ticks):
    localtime = time.localtime(ticks)
    year = localtime[0]
    month = localtime[1]
    day = localtime[2]
    return Date(year, month, day)

def TimeFromTicks(ticks):
    localtime = time.localtime(ticks)
    hour = localtime[3]
    minute = localtime[4]
    second = localtime[5]
    return Time(hour, minute, second)

def TimestampFromTicks(ticks):
    localtime = time.localtime(ticks)
    year = localtime[0]
    month = localtime[1]
    day = localtime[2]
    hour = localtime[3]
    minute = localtime[4]
    second = localtime[5]
    return Timestamp(year, month, day, hour, minute, second)

def Binary(data):
    return buffer(data)

#
# Decimal
#
Decimal = decimal.Decimal

#
# type objects
#
class _AbstractType:
    def __init__(self, name, typeobjects):
        self.name = name
        self.typeobjects = typeobjects

    def __str__(self):
        return self.name

    def __cmp__(self, other):
        if other in self.typeobjects:
            return 0
        else:
            return -1

    def __eq__(self, other):
        return (other in self.typeobjects)

    def __hash__(self):
        return hash(self.name)

NUMBER = _AbstractType('NUMBER', (int, long, float, complex))
DATETIME = _AbstractType('DATETIME', (type(datetime.time(0)), type(datetime.date(1,1,1)), type(datetime.datetime(1,1,1))))
STRING = str
BINARY = buffer
ROWID = int
