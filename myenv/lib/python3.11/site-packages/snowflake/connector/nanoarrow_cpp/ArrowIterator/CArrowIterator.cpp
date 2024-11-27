//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#include "CArrowIterator.hpp"

#include <memory>

#include "nanoarrow.h"
#include "nanoarrow_ipc.h"

namespace sf {

const char* const NANOARROW_TYPE_ENUM_STRING[] = {
    "NANOARROW_TYPE_UNINITIALIZED",
    "NANOARROW_TYPE_NA",
    "NANOARROW_TYPE_BOOL",
    "NANOARROW_TYPE_UINT8",
    "NANOARROW_TYPE_INT8",
    "NANOARROW_TYPE_UINT16",
    "NANOARROW_TYPE_INT16",
    "NANOARROW_TYPE_UINT32",
    "NANOARROW_TYPE_INT32",
    "NANOARROW_TYPE_UINT64",
    "NANOARROW_TYPE_INT64",
    "NANOARROW_TYPE_HALF_FLOAT",
    "NANOARROW_TYPE_FLOAT",
    "NANOARROW_TYPE_DOUBLE",
    "NANOARROW_TYPE_STRING",
    "NANOARROW_TYPE_BINARY",
    "NANOARROW_TYPE_FIXED_SIZE_BINARY",
    "NANOARROW_TYPE_DATE32",
    "NANOARROW_TYPE_DATE64",
    "NANOARROW_TYPE_TIMESTAMP",
    "NANOARROW_TYPE_TIME32",
    "NANOARROW_TYPE_TIME64",
    "NANOARROW_TYPE_INTERVAL_MONTHS",
    "NANOARROW_TYPE_INTERVAL_DAY_TIME",
    "NANOARROW_TYPE_DECIMAL128",
    "NANOARROW_TYPE_DECIMAL256",
    "NANOARROW_TYPE_LIST",
    "NANOARROW_TYPE_STRUCT",
    "NANOARROW_TYPE_SPARSE_UNION",
    "NANOARROW_TYPE_DENSE_UNION",
    "NANOARROW_TYPE_DICTIONARY",
    "NANOARROW_TYPE_MAP",
    "NANOARROW_TYPE_EXTENSION",
    "NANOARROW_TYPE_FIXED_SIZE_LIST",
    "NANOARROW_TYPE_DURATION",
    "NANOARROW_TYPE_LARGE_STRING",
    "NANOARROW_TYPE_LARGE_BINARY",
    "NANOARROW_TYPE_LARGE_LIST",
    "NANOARROW_TYPE_INTERVAL_MONTH_DAY_NANO"};

Logger* CArrowIterator::logger =
    new Logger("snowflake.connector.CArrowIterator");

CArrowIterator::CArrowIterator(char* arrow_bytes, int64_t arrow_bytes_size) {
  int returnCode = 0;
  ArrowBuffer input_buffer;
  ArrowBufferInit(&input_buffer);
  returnCode = ArrowBufferAppend(&input_buffer, arrow_bytes, arrow_bytes_size);
  SF_CHECK_ARROW_RC(
      returnCode,
      "[Snowflake Exception] error loading arrow bytes, error code: %d",
      returnCode);
  ArrowIpcInputStream input;
  returnCode = ArrowIpcInputStreamInitBuffer(&input, &input_buffer);
  SF_CHECK_ARROW_RC(returnCode,
                    "[Snowflake Exception] error initializing "
                    "ArrowIpcInputStream, error code: %d",
                    returnCode);
  ArrowArrayStream stream;
  returnCode = ArrowIpcArrayStreamReaderInit(&stream, &input, nullptr);
  SF_CHECK_ARROW_RC_AND_RELEASE_ARROW_STREAM(
      returnCode, stream,
      "[Snowflake Exception] error initializing ArrowIpcArrayStreamReader, "
      "error code: %d",
      returnCode);
  returnCode = stream.get_schema(&stream, m_ipcArrowSchema.get());
  SF_CHECK_ARROW_RC_AND_RELEASE_ARROW_STREAM(
      returnCode, stream,
      "[Snowflake Exception] error getting schema from stream, error code: %d",
      returnCode);

  while (true) {
    nanoarrow::UniqueArray newUniqueArray;
    nanoarrow::UniqueArrayView newUniqueArrayView;
    auto retcode = stream.get_next(&stream, newUniqueArray.get());
    if (retcode == NANOARROW_OK && newUniqueArray->release != nullptr) {
      m_ipcArrowArrayVec.push_back(std::move(newUniqueArray));

      ArrowError error;
      returnCode = ArrowArrayViewInitFromSchema(newUniqueArrayView.get(),
                                                m_ipcArrowSchema.get(), &error);
      SF_CHECK_ARROW_RC_AND_RELEASE_ARROW_STREAM(
          returnCode, stream,
          "[Snowflake Exception] error initializing ArrowArrayView from schema "
          ": %s, error code: %d",
          ArrowErrorMessage(&error), returnCode);

      returnCode = ArrowArrayViewSetArray(newUniqueArrayView.get(),
                                          newUniqueArray.get(), &error);
      SF_CHECK_ARROW_RC_AND_RELEASE_ARROW_STREAM(
          returnCode, stream,
          "[Snowflake Exception] error setting ArrowArrayView from array : %s, "
          "error code: %d",
          ArrowErrorMessage(&error), returnCode);
      m_ipcArrowArrayViewVec.push_back(std::move(newUniqueArrayView));
    } else {
      SF_CHECK_ARROW_RC_AND_RELEASE_ARROW_STREAM(
          retcode, stream,
          "[Snowflake Exception] error getting schema from stream, error code: "
          "%d",
          returnCode);
      break;
    }
  }
  stream.release(&stream);
  logger->debug(__FILE__, __func__, __LINE__, "Arrow BatchSize: %d",
                m_ipcArrowArrayVec.size());
}

ReturnVal CArrowIterator::checkInitializationStatus() {
  SF_CHECK_PYTHON_ERR()
  return ReturnVal(nullptr, nullptr);
}

}  // namespace sf
