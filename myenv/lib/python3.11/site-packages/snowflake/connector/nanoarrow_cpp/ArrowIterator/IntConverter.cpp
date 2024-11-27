//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#include "IntConverter.hpp"

namespace sf {
/** this file is here for future use and if this is useless at the end, it will
 * be removed */

PyObject* IntConverter::toPyObject(int64_t rowIndex) const {
  if (ArrowArrayViewIsNull(m_array, rowIndex)) {
    Py_RETURN_NONE;
  }
  int64_t val = ArrowArrayViewGetIntUnsafe(m_array, rowIndex);
  return pyLongForward(val);
}

PyObject* NumpyIntConverter::toPyObject(int64_t rowIndex) const {
  if (ArrowArrayViewIsNull(m_array, rowIndex)) {
    Py_RETURN_NONE;
  }
  int64_t val = ArrowArrayViewGetIntUnsafe(m_array, rowIndex);
  return PyObject_CallMethod(m_context, "FIXED_to_numpy_int64", "L", val);
}

}  // namespace sf
