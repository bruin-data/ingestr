//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#include "FloatConverter.hpp"

#include <memory>

namespace sf {

/** snowflake float is 64-precision, which refers to double here */
FloatConverter::FloatConverter(ArrowArrayView* array) : m_array(array) {}

PyObject* FloatConverter::toPyObject(int64_t rowIndex) const {
  if (ArrowArrayViewIsNull(m_array, rowIndex)) {
    Py_RETURN_NONE;
  }
  return PyFloat_FromDouble(ArrowArrayViewGetDoubleUnsafe(m_array, rowIndex));
}

NumpyFloat64Converter::NumpyFloat64Converter(ArrowArrayView* array,
                                             PyObject* context)
    : m_array(array), m_context(context) {}

PyObject* NumpyFloat64Converter::toPyObject(int64_t rowIndex) const {
  if (ArrowArrayViewIsNull(m_array, rowIndex)) {
    Py_RETURN_NONE;
  }

  double val = ArrowArrayViewGetDoubleUnsafe(m_array, rowIndex);
  return PyObject_CallMethod(m_context, "REAL_to_numpy_float64", "d", val);
}

}  // namespace sf
