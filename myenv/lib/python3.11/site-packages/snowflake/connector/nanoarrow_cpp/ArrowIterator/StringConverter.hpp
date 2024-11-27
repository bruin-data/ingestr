//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#ifndef PC_STRINGCONVERTER_HPP
#define PC_STRINGCONVERTER_HPP

#include <memory>

#include "IColumnConverter.hpp"
#include "logging.hpp"
#include "nanoarrow.h"
#include "nanoarrow.hpp"

namespace sf {

class StringConverter : public IColumnConverter {
 public:
  explicit StringConverter(ArrowArrayView* array);
  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  ArrowArrayView* m_array;

  static Logger* logger;
};

}  // namespace sf

#endif  // PC_STRINGCONVERTER_HPP
