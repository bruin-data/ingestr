//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#ifndef PC_FIXEDSIZELISTCONVERTER_HPP
#define PC_FIXEDSIZELISTCONVERTER_HPP

#include <memory>

#include "IColumnConverter.hpp"
#include "logging.hpp"
#include "nanoarrow.h"
#include "nanoarrow.hpp"

namespace sf {

class FixedSizeListConverter : public IColumnConverter {
 public:
  explicit FixedSizeListConverter(ArrowArrayView* array);
  PyObject* toPyObject(int64_t rowIndex) const override;

 private:
  void generateError(const std::string& msg) const;

  ArrowArrayView* m_array;

  static Logger* logger;
};

}  // namespace sf

#endif  // PC_FIXEDSIZELISTCONVERTER_HPP
