#ifndef FLATCC_VERIFIER_H
#define FLATCC_VERIFIER_H

#ifdef __cplusplus
extern "C" {
#endif

/*
 * Runtime support for verifying flatbuffers.
 *
 * Link with the verifier implementation file.
 *
 * Note:
 *
 * 1) nested buffers will NOT have their identifier verified.
 * The user may do so subsequently. The reason is in part because
 * the information is not readily avaible without generated reader code,
 * in part because the buffer might use a different, but valid,
 * identifier and the user has no chance of specifiying this in the
 * verifier code. The root verifier also doesn't assume a specific id
 * but accepts a user supplied input which may be null.
 *
 * 2) All offsets in a buffer are verified for alignment relative to the
 * buffer start, but the buffer itself is only assumed to aligned to
 * uoffset_t. A reader should therefore ensure buffer alignment separately
 * before reading the buffer. Nested buffers are in fact checked for
 * alignment, but still only relative to the root buffer.
 *
 * 3) The max nesting level includes nested buffer nestings, so the
 * verifier might fail even if the individual buffers are otherwise ok.
 * This is to prevent abuse with lots of nested buffers.
 *
 *
 * IMPORTANT:
 *
 * Even if verifier passes, the buffer may be invalid to access due to
 * lack of alignemnt in memory, but the verifier is safe to call.
 *
 * NOTE: The buffer is not safe to modify after verification because an
 * attacker may craft overlapping data structures such that modification
 * of one field updates another in a way that violates the buffer
 * constraints. This may also be caused by a clever compression scheme.
 *
 * It is likely faster to rewrite the table although this is also
 * dangerous because an attacker (or even normal user) can draft a DAG
 * that explodes when expanded carelesslessly. A safer approach is to
 * hash all object references written and reuse those that match. This
 * will expand references into other objects while bounding expansion
 * and it will be safe to update assuming shared objects are ok to
 * update.
 *
 */

#include "flatcc/flatcc_types.h"

#define FLATCC_VERIFY_ERROR_MAP(XX)\
    XX(ok, "ok")\
    XX(buffer_header_too_small, "buffer header too small")\
    XX(identifier_mismatch, "identifier mismatch")\
    XX(max_nesting_level_reached, "max nesting level reached")\
    XX(required_field_missing, "required field missing")\
    XX(runtime_buffer_header_not_aligned, "runtime: buffer header not aligned")\
    XX(runtime_buffer_size_too_large, "runtime: buffer size too large")\
    XX(string_not_zero_terminated, "string not zero terminated")\
    XX(string_out_of_range, "string out of range")\
    XX(struct_out_of_range, "struct out of range")\
    XX(struct_size_overflow, "struct size overflow")\
    XX(struct_unaligned, "struct unaligned")\
    XX(table_field_not_aligned, "table field not aligned")\
    XX(table_field_out_of_range, "table field out of range")\
    XX(table_field_size_overflow, "table field size overflow")\
    XX(table_header_out_of_range_or_unaligned, "table header out of range or unaligned")\
    XX(vector_header_out_of_range_or_unaligned, "vector header out of range or unaligned")\
    XX(string_header_out_of_range_or_unaligned, "string header out of range or unaligned")\
    XX(offset_out_of_range, "offset out of range")\
    XX(table_offset_out_of_range_or_unaligned, "table offset out of range or unaligned")\
    XX(table_size_out_of_range, "table size out of range")\
    XX(type_field_absent_from_required_union_field, "type field absent from required union field")\
    XX(type_field_absent_from_required_union_vector_field, "type field absent from required union vector field")\
    XX(union_cannot_have_a_table_without_a_type, "union cannot have a table without a type")\
    XX(union_type_NONE_cannot_have_a_value, "union value field present with type NONE")\
    XX(vector_count_exceeds_representable_vector_size, "vector count exceeds representable vector size")\
    XX(vector_out_of_range, "vector out of range")\
    XX(vtable_header_out_of_range, "vtable header out of range")\
    XX(vtable_header_too_small, "vtable header too small")\
    XX(vtable_offset_out_of_range_or_unaligned, "vtable offset out of range or unaligned")\
    XX(vtable_size_out_of_range_or_unaligned, "vtable size out of range or unaligned")\
    XX(vtable_size_overflow, "vtable size overflow")\
    XX(union_element_absent_without_type_NONE, "union element absent without type NONE")\
    XX(union_element_present_with_type_NONE, "union element present with type NONE")\
    XX(union_vector_length_mismatch, "union type and table vectors have different lengths")\
    XX(union_vector_verification_not_supported, "union vector verification not supported")\
    XX(not_supported, "not supported")


enum flatcc_verify_error_no {
#define XX(no, str) flatcc_verify_error_##no,
    FLATCC_VERIFY_ERROR_MAP(XX)
#undef XX
};

#define flatcc_verify_ok flatcc_verify_error_ok

const char *flatcc_verify_error_string(int err);

/*
 * Type specific table verifier function that checks each known field
 * for existence in the vtable and then calls the appropriate verifier
 * function in this library.
 *
 * The table descriptor values have been verified for bounds, overflow,
 * and alignment, but vtable entries after header must be verified
 * for all fields the table verifier function understands.
 *
 * Calls other typespecific verifier functions recursively whenever a
 * table field, union or table vector is encountered.
 */
typedef struct flatcc_table_verifier_descriptor flatcc_table_verifier_descriptor_t;
struct flatcc_table_verifier_descriptor {
    /* Pointer to buffer. Not assumed to be aligned beyond uoffset_t. */
    const void *buf;
    /* Buffer size. */
    flatbuffers_uoffset_t end;
    /* Time to live: number nesting levels left before failure. */
    int ttl;
    /* Vtable of current table. */
    const void *vtable;
    /* Table offset relative to buffer start */
    flatbuffers_uoffset_t table;
    /* Table end relative to buffer start as per vtable[1] field. */
    flatbuffers_voffset_t tsize;
    /* Size of vtable in bytes. */
    flatbuffers_voffset_t vsize;
};

typedef int flatcc_table_verifier_f(flatcc_table_verifier_descriptor_t *td);

typedef struct flatcc_union_verifier_descriptor flatcc_union_verifier_descriptor_t;

struct flatcc_union_verifier_descriptor {
    /* Pointer to buffer. Not assumed to be aligned beyond uoffset_t. */
    const void *buf;
    /* Buffer size. */
    flatbuffers_uoffset_t end;
    /* Time to live: number nesting levels left before failure. */
    int ttl;
    /* Type of union value to be verified */
    flatbuffers_utype_t type;
    /* Offset relative to buffer start to where union value offset is stored. */
    flatbuffers_uoffset_t base;
    /* Offset of union value relative to base. */
    flatbuffers_uoffset_t offset;
};

typedef int flatcc_union_verifier_f(flatcc_union_verifier_descriptor_t *ud);

/*
 * The `as_root` functions are normally the only functions called
 * explicitly in this interface.
 *
 * If `fid` is null, the identifier is not checked and is allowed to be entirely absent.
 *
 * The buffer must at least be aligned to uoffset_t on systems that
 * require aligned memory addresses. The buffer pointers alignment is
 * not significant to internal verification of the buffer.
 */
int flatcc_verify_struct_as_root(const void *buf, size_t bufsiz, const char *fid,
        size_t size, uint16_t align);

int flatcc_verify_struct_as_typed_root(const void *buf, size_t bufsiz, flatbuffers_thash_t thash,
        size_t size, uint16_t align);

int flatcc_verify_table_as_root(const void *buf, size_t bufsiz, const char *fid,
        flatcc_table_verifier_f *root_tvf);

int flatcc_verify_table_as_typed_root(const void *buf, size_t bufsiz, flatbuffers_thash_t thash,
        flatcc_table_verifier_f *root_tvf);
/*
 * The buffer header is verified by any of the `_as_root` verifiers, but
 * this function may be used as a quick sanity check.
 */
int flatcc_verify_buffer_header(const void *buf, size_t bufsiz, const char *fid);

int flatcc_verify_typed_buffer_header(const void *buf, size_t bufsiz, flatbuffers_thash_t type_hash);

/*
 * The following functions are typically called by a generated table
 * verifier function.
 */

/* Scalar, enum or struct field. */
int flatcc_verify_field(flatcc_table_verifier_descriptor_t *td,
        flatbuffers_voffset_t id, size_t size, uint16_t align);
/* Vector of scalars, enums or structs. */
int flatcc_verify_vector_field(flatcc_table_verifier_descriptor_t *td,
        flatbuffers_voffset_t id, int required, size_t elem_size, uint16_t align, size_t max_count);
int flatcc_verify_string_field(flatcc_table_verifier_descriptor_t *td,
        flatbuffers_voffset_t id, int required);
int flatcc_verify_string_vector_field(flatcc_table_verifier_descriptor_t *td,
        flatbuffers_voffset_t id, int required);
int flatcc_verify_table_field(flatcc_table_verifier_descriptor_t *td,
    flatbuffers_voffset_t id, int required, flatcc_table_verifier_f tvf);
int flatcc_verify_table_vector_field(flatcc_table_verifier_descriptor_t *td,
    flatbuffers_voffset_t id, int required, flatcc_table_verifier_f tvf);
/* Table verifiers pass 0 as fid. */
int flatcc_verify_struct_as_nested_root(flatcc_table_verifier_descriptor_t *td,
        flatbuffers_voffset_t id, int required, const char *fid,
        size_t size, uint16_t align);
int flatcc_verify_table_as_nested_root(flatcc_table_verifier_descriptor_t *td,
        flatbuffers_voffset_t id, int required, const char *fid,
        uint16_t align, flatcc_table_verifier_f tvf);

/*
 * A NONE type will not accept a table being present, and a required
 * union will not accept a type field being absent, and an absent type
 * field will not accept a table field being present.
 *
 * If the above checks out and the type is not NONE, the uvf callback
 * is executed. It must test each known table type and silently accept
 * any unknown table type for forward compatibility. A union table
 * value is verified without the required flag because an absent table
 * encodes a typed NULL value while an absent type field encodes a
 * missing union which fails if required.
 */
int flatcc_verify_union_field(flatcc_table_verifier_descriptor_t *td,
        flatbuffers_voffset_t id, int required, flatcc_union_verifier_f uvf);

int flatcc_verify_union_vector_field(flatcc_table_verifier_descriptor_t *td,
    flatbuffers_voffset_t id, int required, flatcc_union_verifier_f uvf);

int flatcc_verify_union_table(flatcc_union_verifier_descriptor_t *ud, flatcc_table_verifier_f *tvf);
int flatcc_verify_union_struct(flatcc_union_verifier_descriptor_t *ud, size_t size, uint16_t align);
int flatcc_verify_union_string(flatcc_union_verifier_descriptor_t *ud);

#ifdef __cplusplus
}
#endif

#endif /* FLATCC_VERIFIER_H */
