//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#ifndef PC_FLOATCONVERTER_HPP
#define PC_FLOATCONVERTER_HPP

#include <memory>

#include "IColumnConverter.hpp"
#include "nanoarrow.h"

namespace sf {

class FloatConverter : public IColumnConverter {
 public:
  explicit FloatConverter(ArrowArrayView* array);

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  ArrowArrayView* m_array;
};

class NumpyFloat64Converter : public IColumnConverter {
 public:
  explicit NumpyFloat64Converter(ArrowArrayView* array, PyObject* context);

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  ArrowArrayView* m_array;

  PyObject* m_context;
};

}  // namespace sf

#endif  // PC_FLOATCONVERTER_HPP
