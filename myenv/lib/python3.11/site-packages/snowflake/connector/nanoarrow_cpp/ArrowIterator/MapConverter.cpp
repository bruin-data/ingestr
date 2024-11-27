//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#include "MapConverter.hpp"

#include <memory>

#include "CArrowChunkIterator.hpp"
#include "CArrowIterator.hpp"
#include "SnowflakeType.hpp"

namespace sf {
Logger* MapConverter::logger = new Logger("snowflake.connector.MapConverter");

void MapConverter::generateError(const std::string& msg) const {
  logger->error(__FILE__, __func__, __LINE__, msg.c_str());
  PyErr_SetString(PyExc_Exception, msg.c_str());
}

MapConverter::MapConverter(ArrowSchemaView* schemaView, ArrowArrayView* array,
                           PyObject* context, bool useNumpy) {
  m_array = array;

  if (schemaView->schema->n_children != 1) {
    std::string errorInfo = Logger::formatString(
        "[Snowflake Exception] invalid arrow schema for map entries expected 1 "
        "schema child, but got %d",
        schemaView->schema->n_children);
    this->generateError(errorInfo);
    return;
  }

  ArrowSchema* entries = schemaView->schema->children[0];

  if (entries->n_children != 2) {
    std::string errorInfo = Logger::formatString(
        "[Snowflake Exception] invalid arrow schema for map key/value pair "
        "expected 2 entries, but got %d",
        entries->n_children);
    this->generateError(errorInfo);
    return;
  }

  ArrowSchema* key_schema = entries->children[0];
  ArrowArrayView* key_array = array->children[0]->children[0];
  m_key_converter =
      getConverterFromSchema(key_schema, key_array, context, useNumpy, logger);

  ArrowSchema* value_schema = entries->children[1];
  ArrowArrayView* value_array = array->children[0]->children[1];
  m_value_converter = getConverterFromSchema(value_schema, value_array, context,
                                             useNumpy, logger);
}

PyObject* MapConverter::toPyObject(int64_t rowIndex) const {
  if (ArrowArrayViewIsNull(m_array, rowIndex)) {
    Py_RETURN_NONE;
  }

  // Map ArrowArrays have two child Arrays that contain the the keys and values.
  // The offsets for how many items belong to each row are stored in the parent
  // array offset buffer. The start and end of a row slice has to be infered
  // from the offsets for each row.
  int start = m_array->buffer_views[1].data.as_int32[rowIndex];
  int end = m_array->children[0]->length;
  if (rowIndex + 1 < m_array->length) {
    end = m_array->buffer_views[1].data.as_int32[rowIndex + 1];
  }

  PyObject* dict = PyDict_New();
  for (int i = start; i < end; i++) {
    PyDict_SetItem(dict, m_key_converter->toPyObject(i),
                   m_value_converter->toPyObject(i));
  }
  return dict;
}

}  // namespace sf
