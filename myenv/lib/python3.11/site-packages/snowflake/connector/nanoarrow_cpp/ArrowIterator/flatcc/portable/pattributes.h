
/*
 * C23 introduces an attribute syntax `[[<attribute>]]`. Prior to that
 * other non-standard syntaxes such as `__attribute__((<attribute>))`
 * and `__declspec(<attribute>)` have been supported by some compiler
 * versions.
 *
 * See also:
 * https://en.cppreference.com/w/c/language/attributes
 *
 * There is no portable way to use C23 attributes in older C standards
 * so in order to use these portably, some macro name needs to be
 * defined for each attribute that either maps to the older supported
 * syntax, or ignores the attribute as appropriate.
 *
 * The Linux kernel defines certain attributes as macros, such as
 * `fallthrough`. When adding attributes it seems reasonable to follow
 * the Linux conventions in lack of any official standard. However, it
 * is not the intention that this file should mirror the Linux
 * attributes 1 to 1.
 *
 * See also:
 * https://github.com/torvalds/linux/blob/master/include/linux/compiler_attributes.h
 *
 * There is a risk that exposed attribute names may lead to name
 * conflicts. A conflicting name can be undefined and if necessary used
 * using `pattribute(<attribute>)`. All attributes can be hidden by
 * defining `PORTABLE_EXPOSE_ATTRIBUTES=0` in which case
 * `pattribute(<attribute>)` can still be used and then if a specific
 * attribute name still needs to be exposed, it can be defined manually
 * like `#define fallthrough pattribute(fallthrough)`.
 */


#ifndef PATTRIBUTES_H
#define PATTRIBUTES_H

#ifdef __cplusplus
extern "C" {
#endif

#ifndef PORTABLE_EXPOSE_ATTRIBUTES
#define PORTABLE_EXPOSE_ATTRIBUTES 1
#endif

#ifdef __has_c_attribute
# define PORTABLE_HAS_C_ATTRIBUTE(x) __has_c_attribute(x)
#else
# define PORTABLE_HAS_C_ATTRIBUTE(x) 0
#endif

#ifdef __has_attribute
# define PORTABLE_HAS_ATTRIBUTE(x) __has_attribute(x)
#else
# define PORTABLE_HAS_ATTRIBUTE(x) 0
#endif


/* https://en.cppreference.com/w/c/language/attributes/fallthrough */
#if PORTABLE_HAS_C_ATTRIBUTE(__fallthrough__)
# define pattribute_fallthrough [[__fallthrough__]]
#elif PORTABLE_HAS_ATTRIBUTE(__fallthrough__)
# define pattribute_fallthrough __attribute__((__fallthrough__))
#else
# define pattribute_fallthrough ((void)0)
#endif


#define pattribute(x) pattribute_##x

#if PORTABLE_EXPOSE_ATTRIBUTES

#ifndef fallthrough
# define fallthrough pattribute(fallthrough)
#endif

#endif


#ifdef __cplusplus
}
#endif

#endif /* PATTRIBUTES_H */
