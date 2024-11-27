//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#ifndef PC_TIMESTAMPCONVERTER_HPP
#define PC_TIMESTAMPCONVERTER_HPP

#include <memory>

#include "IColumnConverter.hpp"
#include "Python/Common.hpp"
#include "Python/Helpers.hpp"
#include "Util/time.hpp"
#include "logging.hpp"
#include "nanoarrow.h"

namespace sf {

// correspond to python datetime.time and datetime.time has only support 6 bit
// precision, which is millisecond

class TimeStampBaseConverter : public IColumnConverter {
 public:
  TimeStampBaseConverter(PyObject* context, int32_t scale);
  virtual ~TimeStampBaseConverter() = default;

 protected:
  PyObject* m_context;

  int32_t m_scale;
};

class OneFieldTimeStampNTZConverter : public TimeStampBaseConverter {
 public:
  explicit OneFieldTimeStampNTZConverter(ArrowArrayView* array, int32_t scale,
                                         PyObject* context);

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  ArrowArrayView* m_array;
};

class NumpyOneFieldTimeStampNTZConverter : public TimeStampBaseConverter {
 public:
  explicit NumpyOneFieldTimeStampNTZConverter(ArrowArrayView* array,
                                              int32_t scale, PyObject* context);

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  ArrowArrayView* m_array;
};

class TwoFieldTimeStampNTZConverter : public TimeStampBaseConverter {
 public:
  explicit TwoFieldTimeStampNTZConverter(ArrowArrayView* array,
                                         ArrowSchemaView* schema, int32_t scale,
                                         PyObject* context);

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  ArrowArrayView* m_array;
  ArrowArrayView* m_epoch;
  ArrowArrayView* m_fraction;

  static Logger* logger;
};

class NumpyTwoFieldTimeStampNTZConverter : public TimeStampBaseConverter {
 public:
  explicit NumpyTwoFieldTimeStampNTZConverter(ArrowArrayView* array,
                                              ArrowSchemaView* schema,
                                              int32_t scale, PyObject* context);

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  ArrowArrayView* m_array;
  ArrowArrayView* m_epoch;
  ArrowArrayView* m_fraction;

  static Logger* logger;
};

class OneFieldTimeStampLTZConverter : public TimeStampBaseConverter {
 public:
  explicit OneFieldTimeStampLTZConverter(ArrowArrayView* array, int32_t scale,
                                         PyObject* context);

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  ArrowArrayView* m_array;
};

class TwoFieldTimeStampLTZConverter : public TimeStampBaseConverter {
 public:
  explicit TwoFieldTimeStampLTZConverter(ArrowArrayView* array,
                                         ArrowSchemaView* schema, int32_t scale,
                                         PyObject* context);

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  ArrowArrayView* m_array;
  ArrowArrayView* m_epoch;
  ArrowArrayView* m_fraction;

  static Logger* logger;
};

class TwoFieldTimeStampTZConverter : public TimeStampBaseConverter {
 public:
  explicit TwoFieldTimeStampTZConverter(ArrowArrayView* array,
                                        ArrowSchemaView* schema, int32_t scale,
                                        PyObject* context);

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  ArrowArrayView* m_array;
  ArrowArrayView* m_epoch;
  ArrowArrayView* m_timezone;

  static Logger* logger;
};

class ThreeFieldTimeStampTZConverter : public TimeStampBaseConverter {
 public:
  explicit ThreeFieldTimeStampTZConverter(ArrowArrayView* array,
                                          ArrowSchemaView* schema,
                                          int32_t scale, PyObject* context);

  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  ArrowArrayView* m_array;
  ArrowArrayView* m_epoch;
  ArrowArrayView* m_fraction;
  ArrowArrayView* m_timezone;

  static Logger* logger;
};

}  // namespace sf

#endif  // PC_TIMESTAMPCONVERTER_HPP
