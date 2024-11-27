// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

#ifndef NANOARROW_IPC_H_INCLUDED
#define NANOARROW_IPC_H_INCLUDED

#include "nanoarrow.h"

#ifdef NANOARROW_NAMESPACE

#define ArrowIpcCheckRuntime \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcCheckRuntime)
#define ArrowIpcSharedBufferIsThreadSafe \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcSharedBufferIsThreadSafe)
#define ArrowIpcSharedBufferInit \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcSharedBufferInit)
#define ArrowIpcSharedBufferReset \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcSharedBufferReset)
#define ArrowIpcDecoderInit \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcDecoderInit)
#define ArrowIpcDecoderReset \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcDecoderReset)
#define ArrowIpcDecoderPeekHeader \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcDecoderPeekHeader)
#define ArrowIpcDecoderVerifyHeader \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcDecoderVerifyHeader)
#define ArrowIpcDecoderDecodeHeader \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcDecoderDecodeHeader)
#define ArrowIpcDecoderDecodeSchema \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcDecoderDecodeSchema)
#define ArrowIpcDecoderDecodeArrayView \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcDecoderDecodeArrayView)
#define ArrowIpcDecoderDecodeArray \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcDecoderDecodeArray)
#define ArrowIpcDecoderDecodeArrayFromShared \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcDecoderDecodeArrayFromShared)
#define ArrowIpcDecoderSetSchema \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcDecoderSetSchema)
#define ArrowIpcDecoderSetEndianness \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcDecoderSetEndianness)
#define ArrowIpcInputStreamInitBuffer \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcInputStreamInitBuffer)
#define ArrowIpcInputStreamInitFile \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcInputStreamInitFile)
#define ArrowIpcInputStreamMove \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcInputStreamMove)
#define ArrowIpcArrayStreamReaderInit \
  NANOARROW_SYMBOL(NANOARROW_NAMESPACE, ArrowIpcArrayStreamReaderInit)

#endif

#ifdef __cplusplus
extern "C" {
#endif

/// \defgroup nanoarrow_ipc Nanoarrow IPC extension
///
/// Except where noted, objects are not thread-safe and clients should
/// take care to serialize accesses to methods.
///
/// Because this library is intended to be vendored, it provides full type
/// definitions and encourages clients to stack or statically allocate
/// where convenient.
///
/// @{

/// \brief Metadata version enumerator
enum ArrowIpcMetadataVersion {
  NANOARROW_IPC_METADATA_VERSION_V1,
  NANOARROW_IPC_METADATA_VERSION_V2,
  NANOARROW_IPC_METADATA_VERSION_V3,
  NANOARROW_IPC_METADATA_VERSION_V4,
  NANOARROW_IPC_METADATA_VERSION_V5
};

/// \brief Message type enumerator
enum ArrowIpcMessageType {
  NANOARROW_IPC_MESSAGE_TYPE_UNINITIALIZED,
  NANOARROW_IPC_MESSAGE_TYPE_SCHEMA,
  NANOARROW_IPC_MESSAGE_TYPE_DICTIONARY_BATCH,
  NANOARROW_IPC_MESSAGE_TYPE_RECORD_BATCH,
  NANOARROW_IPC_MESSAGE_TYPE_TENSOR,
  NANOARROW_IPC_MESSAGE_TYPE_SPARSE_TENSOR
};

/// \brief Endianness enumerator
enum ArrowIpcEndianness {
  NANOARROW_IPC_ENDIANNESS_UNINITIALIZED,
  NANOARROW_IPC_ENDIANNESS_LITTLE,
  NANOARROW_IPC_ENDIANNESS_BIG
};

/// \brief Compression type enumerator
enum ArrowIpcCompressionType {
  NANOARROW_IPC_COMPRESSION_TYPE_NONE,
  NANOARROW_IPC_COMPRESSION_TYPE_LZ4_FRAME,
  NANOARROW_IPC_COMPRESSION_TYPE_ZSTD
};

/// \brief Feature flag for a stream that uses dictionary replacement
#define NANOARROW_IPC_FEATURE_DICTIONARY_REPLACEMENT 1

/// \brief Feature flag for a stream that uses compression
#define NANOARROW_IPC_FEATURE_COMPRESSED_BODY 2

/// \brief Checks the nanoarrow runtime to make sure the run/build versions
/// match
ArrowErrorCode ArrowIpcCheckRuntime(struct ArrowError* error);

/// \brief A structure representing a reference-counted buffer that may be
/// passed to ArrowIpcDecoderDecodeArrayFromShared().
struct ArrowIpcSharedBuffer {
  struct ArrowBuffer private_src;
};

/// \brief Initialize the contents of a ArrowIpcSharedBuffer struct
///
/// If NANOARROW_OK is returned, the ArrowIpcSharedBuffer takes ownership of
/// src.
ArrowErrorCode ArrowIpcSharedBufferInit(struct ArrowIpcSharedBuffer* shared,
                                        struct ArrowBuffer* src);

/// \brief Release the caller's copy of the shared buffer
///
/// When finished, the caller must relinquish its own copy of the shared data
/// using this function. The original buffer will continue to exist until all
/// ArrowArray objects that refer to it have also been released.
void ArrowIpcSharedBufferReset(struct ArrowIpcSharedBuffer* shared);

/// \brief Check for shared buffer thread safety
///
/// Thread-safe shared buffers require C11 and the stdatomic.h header.
/// If either are unavailable, shared buffers are still possible but
/// the resulting arrays must not be passed to other threads to be released.
int ArrowIpcSharedBufferIsThreadSafe(void);

/// \brief Decoder for Arrow IPC messages
///
/// This structure is intended to be allocated by the caller,
/// initialized using ArrowIpcDecoderInit(), and released with
/// ArrowIpcDecoderReset(). These fields should not be modified
/// by the caller but can be read following a call to
/// ArrowIpcDecoderPeekHeader(), ArrowIpcDecoderVerifyHeader(), or
/// ArrowIpcDecoderDecodeHeader().
struct ArrowIpcDecoder {
  /// \brief The last verified or decoded message type
  enum ArrowIpcMessageType message_type;

  /// \brief The metadata version as indicated by the current schema message
  enum ArrowIpcMetadataVersion metadata_version;

  /// \brief Buffer endianness as indicated by the current schema message
  enum ArrowIpcEndianness endianness;

  /// \brief Arrow IPC Features used as indicated by the current Schema message
  int32_t feature_flags;

  /// \brief Compression used by the current RecordBatch message
  enum ArrowIpcCompressionType codec;

  /// \brief The number of bytes in the current header message
  ///
  /// This value includes the 8 bytes before the start of the header message
  /// content and any padding bytes required to make the header message size
  /// be a multiple of 8 bytes.
  int32_t header_size_bytes;

  /// \brief The number of bytes in the forthcoming body message.
  int64_t body_size_bytes;

  /// \brief Private resources managed by this library
  void* private_data;
};

/// \brief Initialize a decoder
ArrowErrorCode ArrowIpcDecoderInit(struct ArrowIpcDecoder* decoder);

/// \brief Release all resources attached to a decoder
void ArrowIpcDecoderReset(struct ArrowIpcDecoder* decoder);

/// \brief Peek at a message header
///
/// The first 8 bytes of an Arrow IPC message are 0xFFFFFF followed by the size
/// of the header as a little-endian 32-bit integer. ArrowIpcDecoderPeekHeader()
/// reads these bytes and returns ESPIPE if there are not enough remaining bytes
/// in data to read the entire header message, EINVAL if the first 8 bytes are
/// not valid, ENODATA if the Arrow end-of-stream indicator has been reached, or
/// NANOARROW_OK otherwise.
ArrowErrorCode ArrowIpcDecoderPeekHeader(struct ArrowIpcDecoder* decoder,
                                         struct ArrowBufferView data,
                                         struct ArrowError* error);

/// \brief Verify a message header
///
/// Runs ArrowIpcDecoderPeekHeader() to ensure data is sufficiently large but
/// additionally runs flatbuffer verification to ensure that decoding the data
/// will not access memory outside of the buffer specified by data.
/// ArrowIpcDecoderVerifyHeader() will also set decoder.header_size_bytes,
/// decoder.body_size_bytes, decoder.metadata_version, and decoder.message_type.
///
/// Returns as ArrowIpcDecoderPeekHeader() and additionally will
/// return EINVAL if flatbuffer verification fails.
ArrowErrorCode ArrowIpcDecoderVerifyHeader(struct ArrowIpcDecoder* decoder,
                                           struct ArrowBufferView data,
                                           struct ArrowError* error);

/// \brief Decode a message header
///
/// Runs ArrowIpcDecoderPeekHeader() to ensure data is sufficiently large and
/// decodes the content of the message header. If data contains a schema
/// message, decoder.endianness and decoder.feature_flags is set and
/// ArrowIpcDecoderDecodeSchema() can be used to obtain the decoded schema. If
/// data contains a record batch message, decoder.codec is set and a successful
/// call can be followed by a call to ArrowIpcDecoderDecodeArray().
///
/// In almost all cases this should be preceded by a call to
/// ArrowIpcDecoderVerifyHeader() to ensure decoding does not access data
/// outside of the specified buffer.
///
/// Returns EINVAL if the content of the message cannot be decoded or ENOTSUP if
/// the content of the message uses features not supported by this library.
ArrowErrorCode ArrowIpcDecoderDecodeHeader(struct ArrowIpcDecoder* decoder,
                                           struct ArrowBufferView data,
                                           struct ArrowError* error);

/// \brief Decode an ArrowSchema
///
/// After a successful call to ArrowIpcDecoderDecodeHeader(), retrieve an
/// ArrowSchema. The caller is responsible for releasing the schema if
/// NANOARROW_OK is returned.
///
/// Returns EINVAL if the decoder did not just decode a schema message or
/// NANOARROW_OK otherwise.
ArrowErrorCode ArrowIpcDecoderDecodeSchema(struct ArrowIpcDecoder* decoder,
                                           struct ArrowSchema* out,
                                           struct ArrowError* error);

/// \brief Set the ArrowSchema used to decode future record batch messages
///
/// Prepares the decoder for future record batch messages
/// of this type. The decoder takes ownership of schema if NANOARROW_OK is
/// returned. Note that you must call this explicitly after decoding a Schema
/// message (i.e., the decoder does not assume that the last-decoded schema
/// message applies to future record batch messages).
///
/// Returns EINVAL if schema validation fails or NANOARROW_OK otherwise.
ArrowErrorCode ArrowIpcDecoderSetSchema(struct ArrowIpcDecoder* decoder,
                                        struct ArrowSchema* schema,
                                        struct ArrowError* error);

/// \brief Set the endianness used to decode future record batch messages
///
/// Prepares the decoder for future record batch messages with the specified
/// endianness. Note that you must call this explicitly after decoding a
/// Schema message (i.e., the decoder does not assume that the last-decoded
/// schema message applies to future record batch messages).
///
/// Returns NANOARROW_OK on success.
ArrowErrorCode ArrowIpcDecoderSetEndianness(struct ArrowIpcDecoder* decoder,
                                            enum ArrowIpcEndianness endianness);

/// \brief Decode an ArrowArrayView
///
/// After a successful call to ArrowIpcDecoderDecodeHeader(), deserialize the
/// content of body into an internally-managed ArrowArrayView and return it.
/// Note that field index does not equate to column index if any columns contain
/// nested types. Use a value of -1 to decode the entire array into a struct.
/// The pointed-to ArrowArrayView is owned by the ArrowIpcDecoder and must not
/// be released.
///
/// For streams that match system endianness and do not use compression, this
/// operation will not perform any heap allocations; however, the buffers
/// referred to by the returned ArrowArrayView are only valid as long as the
/// buffer referred to by body stays valid.
ArrowErrorCode ArrowIpcDecoderDecodeArrayView(struct ArrowIpcDecoder* decoder,
                                              struct ArrowBufferView body,
                                              int64_t i,
                                              struct ArrowArrayView** out,
                                              struct ArrowError* error);

/// \brief Decode an ArrowArray
///
/// After a successful call to ArrowIpcDecoderDecodeHeader(), assemble an
/// ArrowArray given a message body and a field index. Note that field index
/// does not equate to column index if any columns contain nested types. Use a
/// value of -1 to decode the entire array into a struct. The caller is
/// responsible for releasing the array if NANOARROW_OK is returned.
///
/// Returns EINVAL if the decoder did not just decode a record batch message,
/// ENOTSUP if the message uses features not supported by this library, or or
/// NANOARROW_OK otherwise.
ArrowErrorCode ArrowIpcDecoderDecodeArray(
    struct ArrowIpcDecoder* decoder, struct ArrowBufferView body, int64_t i,
    struct ArrowArray* out, enum ArrowValidationLevel validation_level,
    struct ArrowError* error);

/// \brief Decode an ArrowArray from an owned buffer
///
/// This implementation takes advantage of the fact that it can avoid copying
/// individual buffers. In all cases the caller must ArrowIpcSharedBufferReset()
/// body after one or more calls to ArrowIpcDecoderDecodeArrayFromShared(). If
/// ArrowIpcSharedBufferIsThreadSafe() returns 0, out must not be released by
/// another thread.
ArrowErrorCode ArrowIpcDecoderDecodeArrayFromShared(
    struct ArrowIpcDecoder* decoder, struct ArrowIpcSharedBuffer* shared,
    int64_t i, struct ArrowArray* out,
    enum ArrowValidationLevel validation_level, struct ArrowError* error);

/// \brief An user-extensible input data source
struct ArrowIpcInputStream {
  /// \brief Read up to buf_size_bytes from stream into buf
  ///
  /// The actual number of bytes read is placed in the value pointed to by
  /// size_read_out. Returns NANOARROW_OK on success.
  ArrowErrorCode (*read)(struct ArrowIpcInputStream* stream, uint8_t* buf,
                         int64_t buf_size_bytes, int64_t* size_read_out,
                         struct ArrowError* error);

  /// \brief Release the stream and any resources it may be holding
  ///
  /// Release callback implementations must set the release member to NULL.
  /// Callers must check that the release callback is not NULL before calling
  /// read() or release().
  void (*release)(struct ArrowIpcInputStream* stream);

  /// \brief Private implementation-defined data
  void* private_data;
};

/// \brief Transfer ownership of an ArrowIpcInputStream
void ArrowIpcInputStreamMove(struct ArrowIpcInputStream* src,
                             struct ArrowIpcInputStream* dst);

/// \brief Create an input stream from an ArrowBuffer
ArrowErrorCode ArrowIpcInputStreamInitBuffer(struct ArrowIpcInputStream* stream,
                                             struct ArrowBuffer* input);

/// \brief Create an input stream from a C FILE* pointer
///
/// Note that the ArrowIpcInputStream has no mechanism to communicate an error
/// if file_ptr fails to close. If this behaviour is needed, pass false to
/// close_on_release and handle closing the file independently from stream.
ArrowErrorCode ArrowIpcInputStreamInitFile(struct ArrowIpcInputStream* stream,
                                           void* file_ptr,
                                           int close_on_release);

/// \brief Options for ArrowIpcArrayStreamReaderInit()
struct ArrowIpcArrayStreamReaderOptions {
  /// \brief The field index to extract.
  ///
  /// Defaults to -1 (i.e., read all fields). Note that this field index refers
  /// to the flattened tree of children and not necessarily the column index.
  int64_t field_index;

  /// \brief Set to a non-zero value to share the message body buffer among
  /// decoded arrays
  ///
  /// Sharing buffers is a good choice when (1) using memory-mapped IO
  /// (since unreferenced portions of the file are often not loaded into memory)
  /// or (2) if all data from all columns are about to be referenced anyway.
  /// When loading a single field there is probably no advantage to using shared
  /// buffers. Defaults to the value of ArrowIpcSharedBufferIsThreadSafe().
  int use_shared_buffers;
};

/// \brief Initialize an ArrowArrayStream from an input stream of bytes
///
/// The stream of bytes must begin with a Schema message and be followed by
/// zero or more RecordBatch messages as described in the Arrow IPC stream
/// format specification. Returns NANOARROW_OK on success. If NANOARROW_OK
/// is returned, the ArrowArrayStream takes ownership of input_stream and
/// the caller is responsible for releasing out.
ArrowErrorCode ArrowIpcArrayStreamReaderInit(
    struct ArrowArrayStream* out, struct ArrowIpcInputStream* input_stream,
    struct ArrowIpcArrayStreamReaderOptions* options);

/// @}

#ifdef __cplusplus
}
#endif

#endif
