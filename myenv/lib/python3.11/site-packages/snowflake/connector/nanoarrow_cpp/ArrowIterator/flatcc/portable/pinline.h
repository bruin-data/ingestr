#ifndef PINLINE_H
#define PINLINE_H

#ifndef __cplusplus

#if (defined(__STDC__) && __STDC__ && defined(__STDC_VERSION__) && __STDC_VERSION__ >= 199901L)
/* C99 or newer */
#elif _MSC_VER >= 1500 /* MSVC 9 or newer */
#undef inline
#define inline __inline
#elif __GNUC__ >= 3 /* GCC 3 or newer */
#define inline __inline
#else /* Unknown or ancient */
#define inline
#endif

#endif /* __cplusplus */

#endif /* PINLINE_H */
