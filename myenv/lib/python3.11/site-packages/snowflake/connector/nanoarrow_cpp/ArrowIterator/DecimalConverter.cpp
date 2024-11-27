//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#include "DecimalConverter.hpp"

#include <memory>
#include <string>

#include "Python/Common.hpp"
#include "Python/Helpers.hpp"

namespace sf {

DecimalBaseConverter::DecimalBaseConverter()
    : m_pyDecimalConstructor(initPyDecimalConstructor()) {}

py::UniqueRef& DecimalBaseConverter::initPyDecimalConstructor() {
  static py::UniqueRef pyDecimalConstructor;
  if (pyDecimalConstructor.empty()) {
    py::UniqueRef decimalModule;
    py::importPythonModule("decimal", decimalModule);
    py::importFromModule(decimalModule, "Decimal", pyDecimalConstructor);
    Py_XINCREF(pyDecimalConstructor.get());
  }

  return pyDecimalConstructor;
}

DecimalFromIntConverter::DecimalFromIntConverter(ArrowArrayView* array,
                                                 int precision, int scale)
    : m_array(array), m_precision(precision), m_scale(scale) {}

PyObject* DecimalFromIntConverter::toPyObject(int64_t rowIndex) const {
  if (ArrowArrayViewIsNull(m_array, rowIndex)) {
    Py_RETURN_NONE;
  }
  int64_t val = ArrowArrayViewGetIntUnsafe(m_array, rowIndex);
  py::UniqueRef decimal(
      PyObject_CallFunction(m_pyDecimalConstructor.get(), "L", val));
  return PyObject_CallMethod(decimal.get(), "scaleb", "i", -m_scale);
}

NumpyDecimalConverter::NumpyDecimalConverter(ArrowArrayView* array,
                                             int precision, int scale,
                                             PyObject* context)
    : m_array(array),
      m_precision(precision),
      m_scale(scale),
      m_context(context) {}

PyObject* NumpyDecimalConverter::toPyObject(int64_t rowIndex) const {
  if (ArrowArrayViewIsNull(m_array, rowIndex)) {
    Py_RETURN_NONE;
  }
  int64_t val = ArrowArrayViewGetIntUnsafe(m_array, rowIndex);
  return PyObject_CallMethod(m_context, "FIXED_to_numpy_float64", "Li", val,
                             m_scale);
}

DecimalFromDecimalConverter::DecimalFromDecimalConverter(PyObject* context,
                                                         ArrowArrayView* array,
                                                         int scale)
    : m_array(array), m_context(context), m_scale(scale) {}

PyObject* DecimalFromDecimalConverter::toPyObject(int64_t rowIndex) const {
  if (ArrowArrayViewIsNull(m_array, rowIndex)) {
    Py_RETURN_NONE;
  }
  int64_t bytes_start = 16 * (m_array->array->offset + rowIndex);
  const char* ptr_start = m_array->buffer_views[1].data.as_char;
  PyObject* int128_bytes =
      PyBytes_FromStringAndSize(&(ptr_start[bytes_start]), 16);
  /**
  # Alternatively, the decimal conversion can be implemented using the
  ArrowDecimal related APIs in the following # code snippets, however, it's less
  performant than the direct memory manipulation. # The code snippets here is
  for context reference.

  ArrowDecimal arrowDecimal;
  ArrowDecimalInit(&arrowDecimal, 128, precision, scale);
  ArrowArrayViewGetDecimalUnsafe(m_array, rowIndex, &arrowDecimal);
  uint8_t outBytes[16];
  ArrowDecimalGetBytes(&arrowDecimal, outBytes);
  PyObject* int128_bytes = PyBytes_FromStringAndSize(&outBytes, 16);
  */
  PyObject* return_object = PyObject_CallMethod(
      m_context, "DECIMAL128_to_decimal", "Si", int128_bytes, m_scale);
  /**
  int128_bytes is a new referenced created by PyBytes_FromStringAndSize,
  to avoid memory leak we need to free it after usage
  check docs:
     https://docs.python.org/3/c-api/bytes.html#c.PyBytes_FromStringAndSize
     https://docs.python.org/3/c-api/refcounting.html#c.Py_XDECREF

  */
  Py_XDECREF(int128_bytes);
  return return_object;
}

}  // namespace sf
