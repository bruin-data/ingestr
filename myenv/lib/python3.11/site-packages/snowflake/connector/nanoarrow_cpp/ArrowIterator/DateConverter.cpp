//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#include "DateConverter.hpp"

#include <memory>

#include "Python/Helpers.hpp"

namespace sf {
Logger* DateConverter::logger = new Logger("snowflake.connector.DateConverter");

py::UniqueRef& DateConverter::initPyDatetimeDate() {
  static py::UniqueRef pyDatetimeDate;
  if (pyDatetimeDate.empty()) {
    py::UniqueRef pyDatetimeModule;
    py::importPythonModule("datetime", pyDatetimeModule);
    py::importFromModule(pyDatetimeModule, "date", pyDatetimeDate);
    Py_XINCREF(pyDatetimeDate.get());
  }
  return pyDatetimeDate;
}

DateConverter::DateConverter(ArrowArrayView* array)
    : m_array(array), m_pyDatetimeDate(initPyDatetimeDate()) {}

PyObject* DateConverter::toPyObject(int64_t rowIndex) const {
  if (ArrowArrayViewIsNull(m_array, rowIndex)) {
    Py_RETURN_NONE;
  }

  int64_t deltaDays = ArrowArrayViewGetIntUnsafe(m_array, rowIndex);
  return PyObject_CallMethod(m_pyDatetimeDate.get(), "fromordinal", "i",
                             epochDay + deltaDays);
}

NumpyDateConverter::NumpyDateConverter(ArrowArrayView* array, PyObject* context)
    : m_array(array), m_context(context) {}

PyObject* NumpyDateConverter::toPyObject(int64_t rowIndex) const {
  if (ArrowArrayViewIsNull(m_array, rowIndex)) {
    Py_RETURN_NONE;
  }

  int64_t deltaDays = ArrowArrayViewGetIntUnsafe(m_array, rowIndex);
  return PyObject_CallMethod(m_context, "DATE_to_numpy_datetime64", "i",
                             deltaDays);
}

}  // namespace sf
