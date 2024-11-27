//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#ifndef PC_MAPCONVERTER_HPP
#define PC_MAPCONVERTER_HPP

#include <memory>

#include "IColumnConverter.hpp"
#include "logging.hpp"
#include "nanoarrow.h"
#include "nanoarrow.hpp"

namespace sf {

class MapConverter : public IColumnConverter {
 public:
  explicit MapConverter(ArrowSchemaView* schemaView, ArrowArrayView* array,
                        PyObject* context, bool useNumpy);

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  void generateError(const std::string& msg) const;

  ArrowArrayView* m_array;
  std::shared_ptr<sf::IColumnConverter> m_key_converter;
  std::shared_ptr<sf::IColumnConverter> m_value_converter;
  static Logger* logger;
};

}  // namespace sf
#endif  // PC_MAPCONVERTER_HPP
