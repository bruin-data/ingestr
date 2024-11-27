//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#include "TimeConverter.hpp"

namespace sf {

TimeConverter::TimeConverter(ArrowArrayView* array, int32_t scale)
    : m_array(array), m_scale(scale) {}

PyObject* TimeConverter::toPyObject(int64_t rowIndex) const {
  if (ArrowArrayViewIsNull(m_array, rowIndex)) {
    Py_RETURN_NONE;
  }

  int64_t seconds = ArrowArrayViewGetIntUnsafe(m_array, rowIndex);
  using namespace internal;
  py::PyUniqueLock lock;
  return PyObject_CallFunction(m_pyDatetimeTime().get(), "iiii",
                               getHourFromSeconds(seconds, m_scale),
                               getMinuteFromSeconds(seconds, m_scale),
                               getSecondFromSeconds(seconds, m_scale),
                               getMicrosecondFromSeconds(seconds, m_scale));
}

py::UniqueRef& TimeConverter::m_pyDatetimeTime() {
  static py::UniqueRef pyDatetimeTime;
  if (pyDatetimeTime.empty()) {
    py::PyUniqueLock lock;
    py::UniqueRef pyDatetimeModule;
    py::importPythonModule("datetime", pyDatetimeModule);
    /** TODO : to check status here */

    py::importFromModule(pyDatetimeModule, "time", pyDatetimeTime);
  }
  return pyDatetimeTime;
}

}  // namespace sf
