/*
 * Copyright (c) 2016 Mikkel Fahnøe Jørgensen, dvide.com
 *
 * (MIT License)
 * Permission is hereby granted, free of charge, to any person obtaining a copy of
 * this software and associated documentation files (the "Software"), to deal in
 * the Software without restriction, including without limitation the rights to
 * use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
 * the Software, and to permit persons to whom the Software is furnished to do so,
 * subject to the following conditions:
 * - The above copyright notice and this permission notice shall be included in
 *   all copies or substantial portions of the Software.
 * - The Software is provided "as is", without warranty of any kind, express or
 *   implied, including but not limited to the warranties of merchantability,
 *   fitness for a particular purpose and noninfringement. In no event shall the
 *   authors or copyright holders be liable for any claim, damages or other
 *   liability, whether in an action of contract, tort or otherwise, arising from,
 *   out of or in connection with the Software or the use or other dealings in the
 *   Software.
 */

#ifndef PUNLIGNED_H
#define PUNLIGNED_H

#ifdef __cplusplus
extern "C" {
#endif

#ifndef PORTABLE_UNALIGNED_ACCESS

#if defined(__i386__) || defined(__x86_64__) || defined(_M_IX86) || defined(_M_X64)
#define PORTABLE_UNALIGNED_ACCESS 1
#else
#define PORTABLE_UNALIGNED_ACCESS 0
#endif

#endif

/* `unaligned_read_16` might not be defined if endianness was not determined. */
#if !defined(unaligned_read_le16toh)

#include "pendian.h"

#ifndef UINT8_MAX
#include <stdint.h>
#endif

#if PORTABLE_UNALIGNED_ACCESS

#define unaligned_read_16(p) (*(uint16_t*)(p))
#define unaligned_read_32(p) (*(uint32_t*)(p))
#define unaligned_read_64(p) (*(uint64_t*)(p))

#define unaligned_read_le16toh(p) le16toh(*(uint16_t*)(p))
#define unaligned_read_le32toh(p) le32toh(*(uint32_t*)(p))
#define unaligned_read_le64toh(p) le64toh(*(uint64_t*)(p))

#define unaligned_read_be16toh(p) be16toh(*(uint16_t*)(p))
#define unaligned_read_be32toh(p) be32toh(*(uint32_t*)(p))
#define unaligned_read_be64toh(p) be64toh(*(uint64_t*)(p))

#define unaligned_write_16(p, v) (*(uint16_t*)(p) = (uint16_t)(v))
#define unaligned_write_32(p, v) (*(uint32_t*)(p) = (uint32_t)(v))
#define unaligned_write_64(p, v) (*(uint64_t*)(p) = (uint64_t)(v))

#define unaligned_write_htole16(p, v) (*(uint16_t*)(p) = htole16(v))
#define unaligned_write_htole32(p, v) (*(uint32_t*)(p) = htole32(v))
#define unaligned_write_htole64(p, v) (*(uint64_t*)(p) = htole64(v))

#define unaligned_write_htobe16(p, v) (*(uint16_t*)(p) = htobe16(v))
#define unaligned_write_htobe32(p, v) (*(uint32_t*)(p) = htobe32(v))
#define unaligned_write_htobe64(p, v) (*(uint64_t*)(p) = htobe64(v))

#else

#define unaligned_read_le16toh(p)  (                                        \
        (((uint16_t)(((uint8_t *)(p))[0])) <<  0) |                         \
        (((uint16_t)(((uint8_t *)(p))[1])) <<  8))

#define unaligned_read_le32toh(p)  (                                        \
        (((uint32_t)(((uint8_t *)(p))[0])) <<  0) |                         \
        (((uint32_t)(((uint8_t *)(p))[1])) <<  8) |                         \
        (((uint32_t)(((uint8_t *)(p))[2])) << 16) |                         \
        (((uint32_t)(((uint8_t *)(p))[3])) << 24))

#define unaligned_read_le64toh(p)  (                                        \
        (((uint64_t)(((uint8_t *)(p))[0])) <<  0) |                         \
        (((uint64_t)(((uint8_t *)(p))[1])) <<  8) |                         \
        (((uint64_t)(((uint8_t *)(p))[2])) << 16) |                         \
        (((uint64_t)(((uint8_t *)(p))[3])) << 24) |                         \
        (((uint64_t)(((uint8_t *)(p))[4])) << 32) |                         \
        (((uint64_t)(((uint8_t *)(p))[5])) << 40) |                         \
        (((uint64_t)(((uint8_t *)(p))[6])) << 48) |                         \
        (((uint64_t)(((uint8_t *)(p))[7])) << 56))

#define unaligned_read_be16toh(p)  (                                        \
        (((uint16_t)(((uint8_t *)(p))[0])) <<  8) |                         \
        (((uint16_t)(((uint8_t *)(p))[1])) <<  0))

#define unaligned_read_be32toh(p)  (                                        \
        (((uint32_t)(((uint8_t *)(p))[0])) << 24) |                         \
        (((uint32_t)(((uint8_t *)(p))[1])) << 16) |                         \
        (((uint32_t)(((uint8_t *)(p))[2])) <<  8) |                         \
        (((uint32_t)(((uint8_t *)(p))[3])) <<  0))

#define unaligned_read_be64toh(p)  (                                        \
        (((uint64_t)(((uint8_t *)(p))[0])) << 56) |                         \
        (((uint64_t)(((uint8_t *)(p))[1])) << 48) |                         \
        (((uint64_t)(((uint8_t *)(p))[2])) << 40) |                         \
        (((uint64_t)(((uint8_t *)(p))[3])) << 32) |                         \
        (((uint64_t)(((uint8_t *)(p))[4])) << 24) |                         \
        (((uint64_t)(((uint8_t *)(p))[5])) << 16) |                         \
        (((uint64_t)(((uint8_t *)(p))[6])) <<  8) |                         \
        (((uint64_t)(((uint8_t *)(p))[7])) <<  0))

#define unaligned_write_htole16(p, v) do {                                  \
        ((uint8_t *)(p))[0] = (uint8_t)(((uint16_t)(v)) >>  0);             \
        ((uint8_t *)(p))[1] = (uint8_t)(((uint16_t)(v)) >>  8);             \
        } while (0)

#define unaligned_write_htole32(p, v) do {                                  \
        ((uint8_t *)(p))[0] = (uint8_t)(((uint32_t)(v)) >>  0);             \
        ((uint8_t *)(p))[1] = (uint8_t)(((uint32_t)(v)) >>  8);             \
        ((uint8_t *)(p))[2] = (uint8_t)(((uint32_t)(v)) >> 16);             \
        ((uint8_t *)(p))[3] = (uint8_t)(((uint32_t)(v)) >> 24);             \
        } while (0)

#define unaligned_write_htole64(p) do {                                     \
        ((uint8_t *)(p))[0] = (uint8_t)(((uint64_t)(v)) >>  0);             \
        ((uint8_t *)(p))[1] = (uint8_t)(((uint64_t)(v)) >>  8);             \
        ((uint8_t *)(p))[2] = (uint8_t)(((uint64_t)(v)) >> 16);             \
        ((uint8_t *)(p))[3] = (uint8_t)(((uint64_t)(v)) >> 24);             \
        ((uint8_t *)(p))[4] = (uint8_t)(((uint64_t)(v)) >> 32);             \
        ((uint8_t *)(p))[5] = (uint8_t)(((uint64_t)(v)) >> 40);             \
        ((uint8_t *)(p))[6] = (uint8_t)(((uint64_t)(v)) >> 48);             \
        ((uint8_t *)(p))[7] = (uint8_t)(((uint64_t)(v)) >> 56);             \
        } while (0)

#define unaligned_write_htobe16(p, v) do {                                  \
        ((uint8_t *)(p))[0] = (uint8_t)(((uint16_t)(v)) >>  8);             \
        ((uint8_t *)(p))[1] = (uint8_t)(((uint16_t)(v)) >>  0);             \
        } while (0)

#define unaligned_write_htobe32(p, v) do {                                  \
        ((uint8_t *)(p))[0] = (uint8_t)(((uint32_t)(v)) >> 24);             \
        ((uint8_t *)(p))[1] = (uint8_t)(((uint32_t)(v)) >> 16);             \
        ((uint8_t *)(p))[2] = (uint8_t)(((uint32_t)(v)) >>  8);             \
        ((uint8_t *)(p))[3] = (uint8_t)(((uint32_t)(v)) >>  0);             \
        } while (0)

#define unaligned_write_htobe64(p) do {                                     \
        ((uint8_t *)(p))[0] = (uint8_t)(((uint64_t)(v)) >> 56);             \
        ((uint8_t *)(p))[1] = (uint8_t)(((uint64_t)(v)) >> 48);             \
        ((uint8_t *)(p))[2] = (uint8_t)(((uint64_t)(v)) >> 40);             \
        ((uint8_t *)(p))[3] = (uint8_t)(((uint64_t)(v)) >> 32);             \
        ((uint8_t *)(p))[4] = (uint8_t)(((uint64_t)(v)) >> 24);             \
        ((uint8_t *)(p))[5] = (uint8_t)(((uint64_t)(v)) >> 16);             \
        ((uint8_t *)(p))[6] = (uint8_t)(((uint64_t)(v)) >>  8);             \
        ((uint8_t *)(p))[7] = (uint8_t)(((uint64_t)(v)) >>  0);             \
        } while (0)

#if __LITTLE_ENDIAN__
#define unaligned_read_16(p) unaligned_read_le16toh(p)
#define unaligned_read_32(p) unaligned_read_le32toh(p)
#define unaligned_read_64(p) unaligned_read_le64toh(p)

#define unaligned_write_16(p) unaligned_write_htole16(p)
#define unaligned_write_32(p) unaligned_write_htole32(p)
#define unaligned_write_64(p) unaligned_write_htole64(p)
#endif

#if __BIG_ENDIAN__
#define unaligned_read_16(p) unaligned_read_be16toh(p)
#define unaligned_read_32(p) unaligned_read_be32toh(p)
#define unaligned_read_64(p) unaligned_read_be64toh(p)

#define unaligned_write_16(p) unaligned_write_htobe16(p)
#define unaligned_write_32(p) unaligned_write_htobe32(p)
#define unaligned_write_64(p) unaligned_write_htobe64(p)
#endif

#endif

#endif

#ifdef __cplusplus
}
#endif

#endif /* PUNALIGNED_H */
