//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#ifndef PC_DECIMALCONVERTER_HPP
#define PC_DECIMALCONVERTER_HPP

#include <memory>

#include "IColumnConverter.hpp"
#include "Python/Common.hpp"
#include "nanoarrow.h"

namespace sf {

class DecimalBaseConverter : public IColumnConverter {
 public:
  DecimalBaseConverter();
  virtual ~DecimalBaseConverter() = default;

 protected:
  py::UniqueRef& m_pyDecimalConstructor;

 private:
  static py::UniqueRef& initPyDecimalConstructor();
};

class DecimalFromDecimalConverter : public DecimalBaseConverter {
 public:
  explicit DecimalFromDecimalConverter(PyObject* context, ArrowArrayView* array,
                                       int scale);

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  ArrowArrayView* m_array;
  PyObject* m_context;
  int m_scale;
  /** no need for this converter to store precision*/
};

class DecimalFromIntConverter : public DecimalBaseConverter {
 public:
  explicit DecimalFromIntConverter(ArrowArrayView* array, int precision,
                                   int scale);

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  ArrowArrayView* m_array;
  int m_precision;  // looks like the precision here is not useful, and this
                    // will be removed soon when it's been confirmed

  int m_scale;
};

class NumpyDecimalConverter : public IColumnConverter {
 public:
  explicit NumpyDecimalConverter(ArrowArrayView* array, int precision,
                                 int scale, PyObject* context);

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  ArrowArrayView* m_array;

  int m_precision;

  int m_scale;

  PyObject* m_context;
};

}  // namespace sf

#endif  // PC_DECIMALCONVERTER_HPP
