#ifndef PORTABLE_BASIC_H
#define PORTABLE_BASIC_H

/*
 * Basic features need to make compilers support the most common moden C
 * features, and endian / unligned read support as well.
 *
 * It is not assumed that this file is always included.
 * Other include files are independent or include what they need.
 */

#include "pversion.h"
#include "pwarnings.h"

/* Featutures that ought to be supported by C11, but some aren't. */
#include "pinttypes.h"
#include "pstdalign.h"
#include "pinline.h"
#include "pstatic_assert.h"

/* These are not supported by C11 and are general platform abstractions. */
#include "pendian.h"
#include "punaligned.h"

#endif /* PORTABLE_BASIC_H */
