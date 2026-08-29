# CSV
ingestr supports CSV files as source and destination. This allows copying data from any database into a local CSV file, as well as uploading data from the local CSV files into database tables.

## URI format
The URI format for CSV files is as follows:

```plaintext
csv://path/to/csv/file.csv
```

## ZIP archives

Append `!<member-glob>` to read selected CSV files from a local ZIP archive:

```plaintext
csv:///path/to/release.zip!data/**/*.csv
```

The same syntax works with the `jsonl://`, `ndjson://`, and `parquet://` source schemes. If the member glob is omitted, all members are selected.

Archive members are processed one at a time without extracting the entire ZIP. The `archive_max_members`, `archive_max_bytes`, `archive_max_uncompressed_bytes`, and `archive_max_expansion_ratio` URI parameters configure safety limits. Their defaults are 10000 members, 10 GiB, 100 GiB, and 1000 respectively.

## Character encoding

ingestr handles UTF-8 (with or without BOM) and UTF-16 LE/BE (with BOM) automatically. If your file is in a different encoding and you see garbled characters (`�` or wrong letters) in the destination, declare the encoding via the `encoding` query parameter:

```plaintext
csv:///path/to/file.csv?encoding=windows-1252
```

To check what encoding your file actually is, inspect the first bytes:

```sh
head -c 4 your_file.csv | xxd
```

| First bytes | Encoding | Action |
|---|---|---|
| `efbb bf` | UTF-8 with BOM | None (auto-detected) |
| `fffe` | UTF-16 LE | None (auto-detected) |
| `feff` | UTF-16 BE | None (auto-detected) |
| `fffe 0000` | UTF-32 LE | `?encoding=utf-32le` |
| `0000 feff` | UTF-32 BE | `?encoding=utf-32be` |
| ASCII-like bytes | UTF-8 or some 8-bit encoding | Try without param; if you see `�`, declare it (most often `windows-1252`) |

Common encoding values: `windows-1252` / `cp1252`, `iso-8859-1` / `latin1`, `windows-1250`, `windows-1251`, `shift_jis`, `gb18030`, `euc-kr`. Names follow the [WHATWG Encoding Standard](https://encoding.spec.whatwg.org/#names-and-labels) (case-insensitive). Unknown names return an error.
