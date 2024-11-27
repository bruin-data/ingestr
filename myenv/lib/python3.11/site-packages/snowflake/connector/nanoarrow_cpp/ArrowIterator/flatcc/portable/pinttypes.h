#ifndef PINTTYPES_H
#define PINTTYPES_H

#ifndef PRId16

#if (defined(__STDC__) && __STDC__ && defined(__STDC_VERSION__) && __STDC_VERSION__ >= 199901L)
/* C99 or newer */
#include <inttypes.h>
#else

/*
 * This is not a complete implementation of <inttypes.h>, just the most
 * useful printf modifiers.
 */

#include "pstdint.h"

#ifndef PRINTF_INT64_MODIFIER
#error "please define PRINTF_INT64_MODIFIER"
#endif

#ifndef PRId64
#define PRId64 PRINTF_INT64_MODIFIER "d"
#define PRIu64 PRINTF_INT64_MODIFIER "u"
#define PRIx64 PRINTF_INT64_MODIFIER "x"
#endif

#ifndef PRINTF_INT32_MODIFIER
#define PRINTF_INT32_MODIFIER "l"
#endif

#ifndef PRId32
#define PRId32 PRINTF_INT32_MODIFIER "d"
#define PRIu32 PRINTF_INT32_MODIFIER "u"
#define PRIx32 PRINTF_INT32_MODIFIER "x"
#endif

#ifndef PRINTF_INT16_MODIFIER
#define PRINTF_INT16_MODIFIER "h"
#endif

#ifndef PRId16
#define PRId16 PRINTF_INT16_MODIFIER "d"
#define PRIu16 PRINTF_INT16_MODIFIER "u"
#define PRIx16 PRINTF_INT16_MODIFIER "x"
#endif

# endif /* __STDC__ */

#endif /* PRId16 */

#endif /* PINTTYPES */
