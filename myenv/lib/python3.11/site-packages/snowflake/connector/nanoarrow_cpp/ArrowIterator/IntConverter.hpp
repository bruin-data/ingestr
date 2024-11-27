//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#ifndef PC_INTCONVERTER_HPP
#define PC_INTCONVERTER_HPP

#include <memory>

#include "IColumnConverter.hpp"
#include "nanoarrow.h"
#include "nanoarrow.hpp"

namespace sf {

class IntConverter : public IColumnConverter {
 public:
  explicit IntConverter(ArrowArrayView* array) : m_array(array) {}

  PyObject* pyLongForward(int64_t value) const {
    return PyLong_FromLongLong(value);
  }

  PyObject* pyLongForward(int32_t value) const {
    return PyLong_FromLong(value);
  }

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  ArrowArrayView* m_array;
};

class NumpyIntConverter : public IColumnConverter {
 public:
  explicit NumpyIntConverter(ArrowArrayView* array, PyObject* context)
      : m_array(array), m_context(context) {}

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  ArrowArrayView* m_array;

  PyObject* m_context;
};

}  // namespace sf

#endif  // PC_INTCONVERTER_HPP
