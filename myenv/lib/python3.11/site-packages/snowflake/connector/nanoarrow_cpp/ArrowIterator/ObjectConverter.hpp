//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//
#ifndef PC_OBJECTCONVERTER_HPP
#define PC_OBJECTCONVERTER_HPP

#include <memory>

#include "IColumnConverter.hpp"
#include "logging.hpp"
#include "nanoarrow.h"
#include "nanoarrow.hpp"

namespace sf {

class ObjectConverter : public IColumnConverter {
 public:
  explicit ObjectConverter(ArrowSchemaView* schemaView, ArrowArrayView* array,
                           PyObject* context, bool useNumpy);
  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  static Logger* logger;
  ArrowArrayView* m_array;
  int m_propertyCount;
  std::vector<const char*> m_property_names;
  std::vector<std::shared_ptr<sf::IColumnConverter>> m_converters;
};

}  // namespace sf

#endif  // PC_OBJECTCONVERTER_HPP
