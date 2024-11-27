//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#ifndef PC_ARROWITERATOR_HPP
#define PC_ARROWITERATOR_HPP

#include <cstdint>
#include <memory>
#include <string>
#include <vector>

#include "Python/Common.hpp"
#include "logging.hpp"
#include "nanoarrow.hpp"

#define SF_CHECK_ARROW_RC(arrow_status, format_string, ...)         \
  if (arrow_status != NANOARROW_OK) {                               \
    std::string errorInfo =                                         \
        Logger::formatString(format_string, ##__VA_ARGS__);         \
    logger->error(__FILE__, __func__, __LINE__, errorInfo.c_str()); \
    PyErr_SetString(PyExc_Exception, errorInfo.c_str());            \
    return;                                                         \
  }

#define SF_CHECK_ARROW_RC_AND_RETURN(arrow_status, ret_val, format_string, \
                                     ...)                                  \
  if (arrow_status != NANOARROW_OK) {                                      \
    std::string errorInfo =                                                \
        Logger::formatString(format_string, ##__VA_ARGS__);                \
    logger->error(__FILE__, __func__, __LINE__, errorInfo.c_str());        \
    PyErr_SetString(PyExc_Exception, errorInfo.c_str());                   \
    return ret_val;                                                        \
  }

#define SF_CHECK_ARROW_RC_AND_RELEASE_ARROW_STREAM(arrow_status, stream, \
                                                   format_string, ...)   \
  if (arrow_status != NANOARROW_OK) {                                    \
    std::string errorInfo = std::string(format_string) +                 \
                            std::string(", error info: ") +              \
                            std::string(stream.get_last_error(&stream)); \
    std::string fullErrorInfo =                                          \
        Logger::formatString(errorInfo.c_str(), ##__VA_ARGS__);          \
    logger->error(__FILE__, __func__, __LINE__, fullErrorInfo.c_str());  \
    PyErr_SetString(PyExc_Exception, fullErrorInfo.c_str());             \
    stream.release(&stream);                                             \
    return;                                                              \
  }

#define SF_CHECK_PYTHON_ERR()                              \
  if (py::checkPyError()) {                                \
    PyObject *type, *val, *traceback;                      \
    PyErr_Fetch(&type, &val, &traceback);                  \
    PyErr_Clear();                                         \
    m_currentPyException.reset(val);                       \
                                                           \
    Py_XDECREF(type);                                      \
    Py_XDECREF(traceback);                                 \
                                                           \
    return ReturnVal(nullptr, m_currentPyException.get()); \
  }

namespace sf {

extern const char* const NANOARROW_TYPE_ENUM_STRING[];

/**
 * A simple struct to contain return data back cython.
 * PyObject would be nullptr if failed and cause string will be populated
 *
 * Note that `ReturnVal` does not own these pointers, so they should
 * not be decref'ed by the receiver.
 */
class ReturnVal {
 public:
  ReturnVal() : successObj(nullptr), exception(nullptr) {}

  ReturnVal(PyObject* obj, PyObject* except)
      : successObj(obj), exception(except) {}

  PyObject* successObj;

  PyObject* exception;
};

/**
 * Arrow base iterator implementation in C++.
 */

class CArrowIterator {
 public:
  CArrowIterator(char* arrow_bytes, int64_t arrow_bytes_size);

  virtual ~CArrowIterator() = default;

  /**
   * @return a python object which might be current row or an Arrow Table
   */
  virtual ReturnVal next() = 0;
  virtual std::vector<uintptr_t> getArrowArrayPtrs() { return {}; };
  virtual std::vector<uintptr_t> getArrowSchemaPtrs() { return {}; };

  /** check whether initialization succeeded or encountered error */
  ReturnVal checkInitializationStatus();

 protected:
  static Logger* logger;

  /** nanoarrow data */
  std::vector<nanoarrow::UniqueArray> m_ipcArrowArrayVec;
  std::vector<nanoarrow::UniqueArrayView> m_ipcArrowArrayViewVec;
  nanoarrow::UniqueSchema m_ipcArrowSchema;

  /** pointer to the current python exception object */
  py::UniqueRef m_currentPyException;
};
}  // namespace sf

#endif  // PC_ARROWITERATOR_HPP
