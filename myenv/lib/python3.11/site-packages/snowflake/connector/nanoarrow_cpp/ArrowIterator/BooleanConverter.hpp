//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#ifndef PC_BOOLEANCONVERTER_HPP
#define PC_BOOLEANCONVERTER_HPP

#include <memory>

#include "IColumnConverter.hpp"
#include "nanoarrow.h"

namespace sf {

class BooleanConverter : public IColumnConverter {
 public:
  explicit BooleanConverter(ArrowArrayView* array);

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  ArrowArrayView* m_array;
};

}  // namespace sf

#endif  // PC_BOOLEANCONVERTER_HPP
