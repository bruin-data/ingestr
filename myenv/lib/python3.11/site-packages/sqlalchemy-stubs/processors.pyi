# fmt: off
from typing import Any
from typing import Optional

from sqlalchemy.cprocessors import DecimalResultProcessor as DecimalResultProcessor
from sqlalchemy.cprocessors import int_to_boolean as int_to_boolean
from sqlalchemy.cprocessors import str_to_date as str_to_date
from sqlalchemy.cprocessors import str_to_datetime as str_to_datetime
from sqlalchemy.cprocessors import str_to_time as str_to_time
from sqlalchemy.cprocessors import to_float as to_float
from sqlalchemy.cprocessors import to_str as to_str
from sqlalchemy.cprocessors import UnicodeResultProcessor as UnicodeResultProcessor
from . import util as util
# fmt: on

def str_to_datetime_processor_factory(regexp: Any, type_: Any): ...
def py_fallback(): ...
def to_unicode_processor_factory(
    encoding: Any, errors: Optional[Any] = ...
): ...
def to_conditional_unicode_processor_factory(
    encoding: Any, errors: Optional[Any] = ...
): ...
def to_decimal_processor_factory(target_class: Any, scale: Any): ...
