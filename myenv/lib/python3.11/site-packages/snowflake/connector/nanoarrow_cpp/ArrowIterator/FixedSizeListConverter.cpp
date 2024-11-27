//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#include "FixedSizeListConverter.hpp"

namespace sf {
Logger* FixedSizeListConverter::logger =
    new Logger("snowflake.connector.FixedSizeListConverter");

FixedSizeListConverter::FixedSizeListConverter(ArrowArrayView* array)
    : m_array(array) {}

void FixedSizeListConverter::generateError(const std::string& msg) const {
  logger->error(__FILE__, __func__, __LINE__, msg.c_str());
  PyErr_SetString(PyExc_Exception, msg.c_str());
}

PyObject* FixedSizeListConverter::toPyObject(int64_t rowIndex) const {
  if (ArrowArrayViewIsNull(m_array, rowIndex)) {
    Py_RETURN_NONE;
  }

  if (m_array->n_children != 1) {
    std::string errorInfo = Logger::formatString(
        "[Snowflake Exception] invalid arrow element schema for fixed size "
        "list: got (%d) "
        "children",
        m_array->n_children);
    this->generateError(errorInfo);
    return nullptr;
  }

  // m_array->length represents the number of fixed size lists in the array
  // m_array->children[0] has a buffer view that contains the actual data of
  // each list, back-to-back m_array->children[0]->length represents the sum of
  // the lengths of the fixed size lists in the array.

  ArrowArrayView* elements = m_array->children[0];
  const auto fixedSizeArrayLength = elements->length / m_array->length;
  PyObject* list = PyList_New(fixedSizeArrayLength);

  const int64_t startIndexWithoutOffset = rowIndex * fixedSizeArrayLength;
  for (int64_t i = 0; i < fixedSizeArrayLength; ++i) {
    const auto bufferIndexWithoutOffset = startIndexWithoutOffset + i;
    // Currently, the backend only sends back INT32 and FLOAT32, but the
    // remaining types are enumerated for future use.
    switch (elements->storage_type) {
      case NANOARROW_TYPE_INT8:
      case NANOARROW_TYPE_INT16:
      case NANOARROW_TYPE_INT32:
      case NANOARROW_TYPE_INT64: {
        const auto value =
            ArrowArrayViewGetIntUnsafe(elements, bufferIndexWithoutOffset);
        PyList_SetItem(list, i, PyLong_FromLongLong(value));
      } break;
      case NANOARROW_TYPE_HALF_FLOAT:
      case NANOARROW_TYPE_FLOAT:
      case NANOARROW_TYPE_DOUBLE: {
        const auto value =
            ArrowArrayViewGetDoubleUnsafe(elements, bufferIndexWithoutOffset);
        PyList_SetItem(list, i, PyFloat_FromDouble(value));
      } break;
      default:
        std::string errorInfo = Logger::formatString(
            "[Snowflake Exception] invalid arrow element type for fixed size "
            "list: got (%s)",
            ArrowTypeString(elements->storage_type));
        this->generateError(errorInfo);
        return nullptr;
    }
  }

  return list;
}

}  // namespace sf
