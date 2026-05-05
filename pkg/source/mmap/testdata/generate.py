"""Generate static Arrow IPC test fixtures for the mmap source tests.

Run once: python3 generate.py
"""

import os
import pyarrow as pa

SCHEMA = pa.schema([
    pa.field("id", pa.int64(), nullable=False),
    pa.field("name", pa.string(), nullable=False),
    pa.field("score", pa.int32(), nullable=False),
    pa.field("is_active", pa.bool_(), nullable=False),
])

def make_batch(id_offset: int, n: int) -> pa.RecordBatch:
    ids = list(range(id_offset, id_offset + n))
    return pa.record_batch(
        [
            pa.array(ids, type=pa.int64()),
            pa.array([f"event_{i:06d}" for i in ids], type=pa.string()),
            pa.array([i % 997 for i in ids], type=pa.int32()),
            pa.array([i % 2 == 0 for i in ids], type=pa.bool_()),
        ],
        schema=SCHEMA,
    )

def write_file(path: str, batches: list[pa.RecordBatch]) -> None:
    with pa.ipc.new_file(path, SCHEMA) as writer:
        for batch in batches:
            writer.write_batch(batch)

here = os.path.dirname(os.path.abspath(__file__))

# Single file: 200 rows in 2 batches of 100
write_file(
    os.path.join(here, "source.arrow"),
    [make_batch(0, 100), make_batch(100, 100)],
)

# Glob files: 3 files, 100 rows each (ids 0-99, 100-199, 200-299)
for i in range(3):
    write_file(
        os.path.join(here, f"part_{i + 1:03d}.arrow"),
        [make_batch(i * 100, 100)],
    )

print("Generated test fixtures in", here)
