//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#include "ArrayConverter.hpp"

#include <memory>

#include "CArrowChunkIterator.hpp"
#include "CArrowIterator.hpp"
#include "SnowflakeType.hpp"

namespace sf {
Logger* ArrayConverter::logger =
    new Logger("snowflake.connector.ArrayConverter");

void ArrayConverter::generateError(const std::string& msg) const {
  logger->error(__FILE__, __func__, __LINE__, msg.c_str());
  PyErr_SetString(PyExc_Exception, msg.c_str());
}

ArrayConverter::ArrayConverter(ArrowSchemaView* schemaView,
                               ArrowArrayView* array, PyObject* context,
                               bool useNumpy) {
  m_array = array;

  if (schemaView->schema->n_children != 1) {
    std::string errorInfo = Logger::formatString(
        "[Snowflake Exception] invalid arrow schema for array items expected 1 "
        "schema child, but got %d",
        schemaView->schema->n_children);
    this->generateError(errorInfo);
    return;
  }

  ArrowSchema* item_schema = schemaView->schema->children[0];
  ArrowArrayView* item_array = array->children[0];
  m_item_converter = getConverterFromSchema(item_schema, item_array, context,
                                            useNumpy, logger);
}

PyObject* ArrayConverter::toPyObject(int64_t rowIndex) const {
  if (ArrowArrayViewIsNull(m_array, rowIndex)) {
    Py_RETURN_NONE;
  }

  // Array item offsets are stored in the second array buffers
  // Infer start an end of this rows slice by looking at the
  // current and next offset. If there isn't another offset use
  // the end of the array instead.
  int start = m_array->buffer_views[1].data.as_int32[rowIndex];
  int end = m_array->children[0]->length;
  if (rowIndex + 1 < m_array->length) {
    end = m_array->buffer_views[1].data.as_int32[rowIndex + 1];
  }

  PyObject* list = PyList_New(end - start);
  for (int i = start; i < end; i++) {
    PyList_SetItem(list, i - start, m_item_converter->toPyObject(i));
  }
  return list;
}

}  // namespace sf
