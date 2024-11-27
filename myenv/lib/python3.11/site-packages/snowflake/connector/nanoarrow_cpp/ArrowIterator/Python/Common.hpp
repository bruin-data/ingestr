//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#ifndef PC_PYTHON_COMMON_HPP
#define PC_PYTHON_COMMON_HPP

// Support for not having PY_SSIZE_T_CLEAN defined will end in Python 3.10. It
// causes
//  argument parsing to not accept integers, leaving only Py_ssize_t as an
//  option
#define PY_SSIZE_T_CLEAN

// We have to make sure that we import Python.h once for special flags that need
// to be
//  set before importing it
#include <Python.h>

#include "Util/macros.hpp"

namespace sf {

namespace py {
inline bool checkPyError() { return UNLIKELY(PyErr_Occurred()); }

/**
 * A RAII class to wrap the PyObject*. The semantics are like std::unique_ptr.
 */
class UniqueRef {
 public:
  UniqueRef(const UniqueRef&) = delete;
  UniqueRef& operator=(const UniqueRef&) = delete;

  UniqueRef() : m_pyObj(nullptr) {}

  explicit UniqueRef(PyObject* pyObj) : m_pyObj(pyObj) {}

  UniqueRef(UniqueRef&& other) : UniqueRef(other.release()) {}

  UniqueRef& operator=(UniqueRef&& other) {
    reset(other.release());
    return *this;
  }

  ~UniqueRef() { reset(); }

  // Make this `UniqueRef` take ownership of the reference
  // `pyObj`, if it is not null. If this `UniqueRef` previously
  // owned a reference, it is released (via decrementing the
  // refcount).
  //
  // This provides a similar guarantee as `Py_SETREF()` or
  // `Py_CLEAR()`, by setting the new reference before
  // decrementing the old reference.
  void reset(PyObject* pyObj = nullptr) {
    PyObject* toDelete = m_pyObj;
    m_pyObj = pyObj;
    Py_XDECREF(toDelete);
  }

  PyObject* release() noexcept {
    PyObject* tmp = m_pyObj;
    m_pyObj = nullptr;
    return tmp;
  }

  PyObject* get() const noexcept { return m_pyObj; }

  bool empty() const noexcept { return m_pyObj == nullptr; }

 private:
  PyObject* m_pyObj;
};

/**
 * A RAII class to help us acquire the python GIL. The semantics are like
 * std::unique_lock.
 * We have to acquire the python GIL every time we call a Python/C API to ensure
 * there is only one python thread running all the time.
 */
class PyUniqueLock {
 public:
  PyUniqueLock(const PyUniqueLock&) = delete;
  PyUniqueLock& operator=(const PyUniqueLock&) = delete;
  PyUniqueLock(PyUniqueLock&&) = delete;
  PyUniqueLock& operator=(PyUniqueLock&&) = delete;

  PyUniqueLock() { m_state = PyGILState_Ensure(); }

  ~PyUniqueLock() { PyGILState_Release(m_state); }

 private:
  PyGILState_STATE m_state;
};

}  // namespace py
}  // namespace sf

#endif  // PC_PYTHON_COMMON_HPP
