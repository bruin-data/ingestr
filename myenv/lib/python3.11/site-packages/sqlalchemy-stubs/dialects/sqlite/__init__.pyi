from typing import Any

from .base import BLOB as BLOB
from .base import BOOLEAN as BOOLEAN
from .base import CHAR as CHAR
from .base import DATE as DATE
from .base import DATETIME as DATETIME
from .base import DECIMAL as DECIMAL
from .base import FLOAT as FLOAT
from .base import INTEGER as INTEGER
from .base import JSON as JSON
from .base import NUMERIC as NUMERIC
from .base import REAL as REAL
from .base import SMALLINT as SMALLINT
from .base import TEXT as TEXT
from .base import TIME as TIME
from .base import TIMESTAMP as TIMESTAMP
from .base import VARCHAR as VARCHAR
from .dml import Insert as Insert
from .dml import insert as insert

dialect: Any
