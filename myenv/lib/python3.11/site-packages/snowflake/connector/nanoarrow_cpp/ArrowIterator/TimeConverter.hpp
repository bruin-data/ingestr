//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#ifndef PC_TIMECONVERTER_HPP
#define PC_TIMECONVERTER_HPP

#include <memory>

#include "IColumnConverter.hpp"
#include "Python/Common.hpp"
#include "Python/Helpers.hpp"
#include "Util/time.hpp"
#include "nanoarrow.h"

namespace sf {

class TimeConverter : public IColumnConverter {
 public:
  explicit TimeConverter(ArrowArrayView* array, int32_t scale);

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  /** can be arrow::Int32Array and arrow::Int64Array */
  ArrowArrayView* m_array;

  int32_t m_scale;

  static py::UniqueRef& m_pyDatetimeTime();
};

}  // namespace sf

#endif  // PC_TIMECONVERTER_HPP
