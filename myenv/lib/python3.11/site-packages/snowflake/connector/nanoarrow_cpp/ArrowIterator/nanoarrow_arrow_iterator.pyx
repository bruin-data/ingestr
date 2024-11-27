#
# Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
#

# distutils: language = c++
# cython: language_level=3


from cpython.ref cimport PyObject
from cython.operator cimport dereference
from libc.stdint cimport int64_t, uint8_t, uintptr_t
from libcpp.vector cimport vector

INSTALLED_PYARROW = False
try:
    import pyarrow
    INSTALLED_PYARROW = True
except ImportError:
    pass

from .constants import IterUnit
from .errorcode import (
    ER_FAILED_TO_CONVERT_ROW_TO_PYTHON_TYPE,
    ER_FAILED_TO_READ_ARROW_STREAM,
    ER_NO_PYARROW,
)
from .errors import Error, InterfaceError, OperationalError, ProgrammingError
from .snow_logging import getSnowLogger

snow_logger = getSnowLogger(__name__)


cdef extern from "CArrowIterator.hpp" namespace "sf":
    cdef cppclass ReturnVal:
        PyObject * successObj;

        PyObject * exception;

    cdef cppclass CArrowIterator:
        ReturnVal next() except +;
        ReturnVal checkInitializationStatus() except +;
        vector[uintptr_t] getArrowArrayPtrs();
        vector[uintptr_t] getArrowSchemaPtrs();


cdef extern from "CArrowChunkIterator.hpp" namespace "sf":
    cdef cppclass CArrowChunkIterator(CArrowIterator):
        CArrowChunkIterator(
            PyObject* context,
            char* arrow_bytes,
            int64_t arrow_bytes_size,
            PyObject* use_numpy,
        ) except +

    cdef cppclass DictCArrowChunkIterator(CArrowChunkIterator):
        DictCArrowChunkIterator(
            PyObject* context,
            char* arrow_bytes,
            int64_t arrow_bytes_size,
            PyObject* use_numpy
        ) except +

cdef extern from "CArrowTableIterator.hpp" namespace "sf":
    cdef cppclass CArrowTableIterator(CArrowIterator):
        CArrowTableIterator(
            PyObject* context,
            char* arrow_bytes,
            int64_t arrow_bytes_size,
            bint number_to_decimal,
        ) except +


cdef class EmptyPyArrowIterator:

    def __iter__(self):
        return self

    def __next__(self):
       raise StopIteration

    def init(self, str iter_unit, bint number_to_decimal):
        pass


cdef class PyArrowIterator(EmptyPyArrowIterator):
    cdef object context
    cdef CArrowIterator* cIterator
    cdef str unit
    cdef object use_dict_result
    cdef object cursor
    cdef vector[uintptr_t] nanoarrow_Table
    cdef vector[uintptr_t] nanoarrow_Schema
    cdef object table_returned
    cdef char* arrow_bytes
    cdef int64_t arrow_bytes_size

    # this is the flag indicating whether fetch data as numpy datatypes or not. The flag
    # is passed from the constructor of SnowflakeConnection class. Note, only FIXED, REAL
    # and TIMESTAMP_NTZ will be converted into numpy data types, all other sql types will
    # still be converted into native python types.
    # https://docs.snowflake.com/en/user-guide/sqlalchemy.html#numpy-data-type-support
    cdef object use_numpy
    cdef object number_to_decimal
    cdef object pyarrow_table

    def __cinit__(
            self,
            object cursor,
            object arrow_bytes,
            object arrow_context,
            object use_dict_result,
            object numpy,
            object number_to_decimal,
    ):
        self.context = arrow_context
        self.cIterator = NULL
        self.use_dict_result = use_dict_result
        self.cursor = cursor
        self.use_numpy = numpy
        self.number_to_decimal = number_to_decimal
        self.pyarrow_table = None
        self.table_returned = False
        self.arrow_bytes = <char*>arrow_bytes
        self.arrow_bytes_size = len(arrow_bytes)

    def __dealloc__(self):
        del self.cIterator

    def __iter__(self):
        return self


cdef class PyArrowRowIterator(PyArrowIterator):
    def __cinit__(
        self,
        object cursor,
        object py_inputstream,
        object arrow_context,
        object use_dict_result,
        object numpy,
        object number_to_decimal,
    ):
        super().__init__(cursor, py_inputstream, arrow_context, use_dict_result, numpy, number_to_decimal)
        if self.cIterator is not NULL:
            return

        self.cIterator = new DictCArrowChunkIterator(
            <PyObject *> self.context,
            self.arrow_bytes,
            self.arrow_bytes_size,
            <PyObject *> self.use_numpy
        ) \
            if self.use_dict_result \
            else new CArrowChunkIterator(
            <PyObject *> self.context,
            self.arrow_bytes,
            self.arrow_bytes_size,
            <PyObject *> self.use_numpy
            )
        cdef ReturnVal cret = self.cIterator.checkInitializationStatus()
        if cret.exception:
            Error.errorhandler_wrapper(
                self.cursor.connection if self.cursor is not None else None,
                self.cursor,
                OperationalError,
                {
                    'msg': f'Failed to open arrow stream: {str(<object>cret.exception)}',
                    'errno': ER_FAILED_TO_READ_ARROW_STREAM
                })
        snow_logger.debug(msg=f"Batches read: {self.cIterator.getArrowArrayPtrs().size()}", path_name=__file__, func_name="__cinit__")

    def __next__(self):
        cdef ReturnVal cret = self.cIterator.next()
        if not cret.successObj:
            Error.errorhandler_wrapper(
                self.cursor.connection if self.cursor is not None else None,
                self.cursor,
                InterfaceError,
                {
                    'msg': f'Failed to convert current row, cause: {<object>cret.exception}',
                    'errno': ER_FAILED_TO_CONVERT_ROW_TO_PYTHON_TYPE
                }
            )
            # it looks like this line can help us get into python and detect the global variable immediately
            # however, this log will not show up for unclear reason
        ret = <object>cret.successObj

        if ret is None:
            raise StopIteration
        else:
            return ret


cdef class PyArrowTableIterator(PyArrowIterator):
    def __cinit__(
        self,
        object cursor,
        object py_inputstream,
        object arrow_context,
        object use_dict_result,
        object numpy,
        object number_to_decimal,
    ):
        super().__init__(cursor, py_inputstream, arrow_context, use_dict_result, numpy, number_to_decimal)
        if not INSTALLED_PYARROW:
            raise Error.errorhandler_make_exception(
                ProgrammingError,
                {
                    "msg": (
                        "Optional dependency: 'pyarrow' is not installed, please see the following link for install "
                        "instructions: https://docs.snowflake.com/en/user-guide/python-connector-pandas.html#installation"
                    ),
                    "errno": ER_NO_PYARROW,
                },
            )

        if self.cIterator is not NULL:
            return

        self.cIterator = new CArrowTableIterator(
            <PyObject *> self.context,
            self.arrow_bytes,
            self.arrow_bytes_size,
            self.number_to_decimal,
        )
        cdef ReturnVal cret = self.cIterator.checkInitializationStatus()
        if cret.exception:
            Error.errorhandler_wrapper(
                self.cursor.connection if self.cursor is not None else None,
                self.cursor,
                OperationalError,
                {
                    'msg': f'Failed to open arrow stream: {str(<object>cret.exception)}',
                    'errno': ER_FAILED_TO_READ_ARROW_STREAM
                })
        cdef ReturnVal cret2 = self.cIterator.next()
        self.nanoarrow_Table = self.cIterator.getArrowArrayPtrs()
        self.nanoarrow_Schema = self.cIterator.getArrowSchemaPtrs()
        self.pyarrow_table = pyarrow.Table.from_batches(
            batches=[
                pyarrow.RecordBatch._import_from_c(
                    self.nanoarrow_Table[i],
                    self.nanoarrow_Schema[i]
                ) for i in range(self.nanoarrow_Table.size())
            ]
        )
        snow_logger.debug(msg=f"Batches read: {self.nanoarrow_Table.size()}", path_name=__file__, func_name="__cinit__")

    def __next__(self):
        if not self.table_returned:
            self.table_returned = True
            return self.pyarrow_table
        raise StopIteration
