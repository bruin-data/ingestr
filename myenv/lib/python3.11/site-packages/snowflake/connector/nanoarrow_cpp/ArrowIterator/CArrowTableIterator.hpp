//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#ifndef PC_ARROWTABLEITERATOR_HPP
#define PC_ARROWTABLEITERATOR_HPP

#include <memory>
#include <string>
#include <vector>

#include "CArrowIterator.hpp"
#include "nanoarrow.h"
#include "nanoarrow.hpp"

namespace sf {

/**
 * Arrow table iterator implementation in C++.
 * The caller will ask for an Arrow Table to be returned back to Python
 * This conversion is zero-copy, just aggregate every columns from multiple
 * record batches and build a new table.
 */
class CArrowTableIterator : public CArrowIterator {
 public:
  /**
   * Constructor
   */
  CArrowTableIterator(PyObject* context, char* arrow_bytes,
                      int64_t arrow_bytes_size, bool number_to_decimal);

  /**
   * Destructor
   */
  ~CArrowTableIterator() = default;

  /**
   * @return an arrow table containing all data in all record batches
   */
  ReturnVal next() override;
  std::vector<uintptr_t> getArrowArrayPtrs() override;
  std::vector<uintptr_t> getArrowSchemaPtrs() override;

 private:
  // nanoarrow data
  std::vector<nanoarrow::UniqueSchema> m_ipcSchemaArrayVec;

  bool m_tableConverted = false;

  /** arrow format convert context for the current session */
  PyObject* m_context;

  /** local time zone */
  char* m_timezone;
  const bool m_convert_number_to_decimal;

  /**
   * Reconstruct record batches with type conversion in place
   */
  void reconstructRecordBatches();
  void convertIfNeeded(ArrowSchema* columnSchema, ArrowArrayView* columnArray);
  void reconstructRecordBatches_nanoarrow();

  /**
   * Convert all current RecordBatches to Arrow Table
   * @return if conversion is executed at first time and successfully
   */
  bool convertRecordBatchesToTable_nanoarrow();

  /**
   * convert scaled fixed number column to Decimal, or Double column based on
   * setting
   */
  void convertScaledFixedNumberColumn_nanoarrow(ArrowSchemaView* field,
                                                ArrowArrayView* columnArray,
                                                const unsigned int scale);

  /**
   * convert scaled fixed number column to Decimal column
   */
  void convertScaledFixedNumberColumnToDecimalColumn_nanoarrow(
      ArrowSchemaView* field, ArrowArrayView* columnArray,
      const unsigned int scale);

  /**
   * convert scaled fixed number column to Double column
   */
  void convertScaledFixedNumberColumnToDoubleColumn_nanoarrow(
      ArrowSchemaView* field, ArrowArrayView* columnArray,
      const unsigned int scale);

  /**
   * convert Snowflake Time column (Arrow int32/int64) to Arrow Time column
   * Since Python/Pandas Time does not support nanoseconds, this function
   * truncates values to microseconds if necessary
   */
  void convertTimeColumn_nanoarrow(ArrowSchemaView* field,
                                   ArrowArrayView* columnArray,
                                   const int scale);

  /**
   * convert Snowflake TimestampNTZ/TimestampLTZ column to Arrow Timestamp
   * column
   */
  void convertTimestampColumn_nanoarrow(ArrowSchemaView* field,
                                        ArrowArrayView* columnArray,
                                        const int scale,
                                        const std::string timezone = "");

  /**
   * convert Snowflake TimestampTZ column to Arrow Timestamp column in UTC
   * Arrow Timestamp does not support time zone info in each value, so this
   * method convert TimestampTZ to Arrow timestamp with UTC timezone
   */
  void convertTimestampTZColumn_nanoarrow(ArrowSchemaView* field,
                                          ArrowArrayView* columnArray,
                                          const int scale, const int byteLength,
                                          const std::string timezone);

  /**
   * convert scaled fixed number to double
   * if scale is small, then just divide based on the scale; otherwise, convert
   * the value to string first and then convert to double to avoid precision
   * loss
   */
  template <typename T>
  double convertScaledFixedNumberToDouble(const unsigned int scale,
                                          T originalValue);
};
}  // namespace sf
#endif  // PC_ARROWTABLEITERATOR_HPP
