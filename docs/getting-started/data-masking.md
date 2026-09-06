# Data Masking

Data masking is a critical security feature that allows you to protect sensitive information while maintaining data utility for development, testing, and analytics purposes. ingestr provides comprehensive masking capabilities that can be applied to any column during the ingestion process.

## Overview

Data masking transforms sensitive data into a protected format while preserving the structure and type of the original data. This is essential for:

- **Compliance** with regulations like GDPR, CCPA, HIPAA
- **Security** in development and testing environments
- **Privacy** protection in analytics and reporting
- **Data sharing** with third parties or external systems

## Usage

Apply masking to specific columns using the `--mask` parameter:

```bash
ingestr ingest \
  --source-uri "postgres://user:pass@localhost/db" \
  --source-table "users" \
  --dest-uri "duckdb:///masked_data.db" \
  --dest-table "masked_users" \
  --mask "email:hash" \
  --mask "ssn:partial:4" \
  --mask "salary:round:1000"
```

### Format

```
--mask <column_name>:<algorithm>[:<parameter>]
```

- `column_name`: The name of the column to mask
- `algorithm`: The masking algorithm to apply
- `parameter`: Optional parameter for algorithms that require configuration

## Masking Algorithms

### Model-based PII detection: `gliner`

Use GLiNER PII Edge to replace detected PII spans **inside a field**, leaving the
surrounding text intact:

```bash
ingestr ingest \
  --source-uri 'csv:///data/messages.csv' \
  --source-table messages \
  --dest-uri 'duckdb:///data/masked.db' \
  --dest-table messages \
  --mask 'message:gliner' \
  --mask 'notes:gliner'
# Contact Maya Chen at maya.chen@example.com or +1 (415) 555-0124.
# → Contact <NAME> at <EMAIL> or <PHONE_NUMBER>.
```

`gliner` accepts no parameter. It uses eight fixed labels at threshold 0.3:
`<NAME>`, `<ADDRESS>`, `<EMAIL>`, `<PHONE_NUMBER>`, `<URL>`, `<DATE>`,
`<ACCOUNT_NUMBER>`, and `<SECRET>`. Overlapping detections are resolved by score.
Output is a string; as with existing string masks, non-string values are processed
as their string representation. Nulls remain null, and empty/whitespace-only
strings are unchanged without inference. Unicode span offsets are UTF-8 bytes.
Invalid UTF-8, more than 2,048 splitter tokens, or more than 7,999 model tokens
(including label prompts) fail ingestion rather than silently truncating input.
There is no long-document chunking. Model failures return errors, not the
unmasked input, and inference errors do not include the field contents.

**Detection is probabilistic and can miss PII or mask non-PII.** This is not a
guarantee of anonymization or regulatory compliance. Prefer `redact` for complete
field removal. Do not use detected-span redaction to preserve primary-key uniqueness.

#### Installation and offline use

Only selecting `gliner` causes model setup. After column validation, ingestion
downloads missing assets, verifies SHA-256 checksums, and initializes the model
before executing the write strategy. Ordinary ingestion, existing masks, help,
and schema/configuration inspection do not download or initialize it.

The first selected run downloads the publisher's **181 MB FP32** ONNX export and
tokenizer files from [knowledgator/gliner-pii-edge-v1.0](https://huggingface.co/knowledgator/gliner-pii-edge-v1.0),
Apache 2.0, pinned to checkpoint `9b7f39b0a2da971a5beea78d35f1539d4009c891`.
Assets are cached under the OS user cache directory at
`ingestr/models/gliner-pii-edge/<checkpoint>/` (on Linux, normally
`~/.cache/ingestr/models/gliner-pii-edge/<checkpoint>/`, respecting `XDG_CACHE_HOME`).
No input text is sent to Hugging Face or another inference service. Downloads
use a 15-minute per-file timeout and honor ingestion cancellation.

For offline runs, set `INGESTR_GLINER_MODEL_DIR` to a pre-provisioned directory
containing `onnx/model.onnx`, `tokenizer.json`, and `tokenizer_config.json` from
that checkpoint. You can copy the downloaded cache directory from an online
machine. An explicit directory **never downloads**; missing or mismatched files
fail with an error. Checksums are verified on initialization, including cached
files. Corrupt cached files must be removed or replaced before retrying.

#### Runtime and performance limits

Inference uses GoMLX's pure-Go backend and `hftokenizer`: no Python subprocess,
CGO, XLA, or ONNX Runtime is needed for this mask. Building ingestr now requires
Go 1.27 or later. The rest of ingestr may still use native database drivers.
The UINT8 export is not supported (`DynamicQuantizeLSTM`); do not substitute it.
The bundled importer compatibility fork retains its license and only adds the
Flatten boundary fix and rank-one ScatterElements support. A separate compute
compatibility fork replaces invalid integer-address arithmetic in matrix
kernels and packing with typed slice indexing; pointer checks remain enabled and no
special build tags are required. The runtime raises
graph constant retention and omits LSTM lengths in memory **only for unpadded
batch-one inputs**.

One tokenizer, parsed model, weight store, and Go backend are reused across the
masked columns and rows of an ingestion job. Calls are serialized, including
concurrent batches; resources are closed when the job ends. Each text gets a
fresh specialized graph, which is released after inference. Inputs/predictions
are not kept in a result cache. Separate ingestion jobs have separate runtimes
but reuse the disk cache. Cancellation is checked between values; an inference
already in progress must finish before cancellation can take effect.

This is intended for low-volume, short text, not high-throughput ingestion.
The original PoC observed about **1.1–1.3 seconds per short fresh request** and
**707 MiB peak RAM** at four threads on a Xeon. Its 324 ms repeated-same-graph
measurement is **not general warm latency**: new text requires graph construction.
The integrated runtime reuses weights, but makes no latency or memory guarantee;
longer inputs and simultaneous jobs can require substantially more memory.
`GOMAXPROCS=4` can limit Go CPU parallelism for the process, not just this mask.

Developer verification (explicitly permits model download; ordinary tests skip it):

```bash
INGESTR_TEST_GLINER=1 GOMAXPROCS=4 CGO_ENABLED=0 \
  go test -count=1 -v ./pkg/transformer/gliner -run TestModelReferenceParity
```

### Irreversible Masking

These algorithms permanently transform data in a way that cannot be reversed.

#### `hash` / `sha256`
Creates a SHA-256 hash of the value. Consistent across runs - the same input always produces the same output.

**Use cases:** Creating anonymous identifiers, consistent tokenization
```bash
--mask "user_id:hash"
# john.doe@example.com → a94a8fe5ccb19ba61c4c0873d391e987982fbbd3
```

#### `md5`
Creates an MD5 hash. Faster than SHA-256 but less secure (adequate for non-security purposes).

**Use cases:** Quick checksums, non-security tokenization
```bash
--mask "session_id:md5"
```

#### `hmac`
Hash-based message authentication code with a secret key. Provides consistent hashing across systems when using the same key.

**Use cases:** Cross-system consistency with shared secret
```bash
--mask "customer_id:hmac:my-secret-key"
```

#### `redact`
Replaces the entire value with "REDACTED".

**Use cases:** Complete removal of sensitive data
```bash
--mask "comments:redact"
# "Customer complaint about..." → "REDACTED"
```

### Format-Preserving Masking

These algorithms maintain the format and structure of the original data.

#### `email`
Masks email addresses while preserving the domain.

**Use cases:** Protecting email addresses while maintaining domain analysis
```bash
--mask "email:email"
# john.doe@example.com → j******e@example.com
```

#### `phone`
Masks phone numbers while preserving country and area codes.

**Use cases:** Geographic analysis without exposing full numbers
```bash
--mask "phone:phone"
# +1-555-123-4567 → +1-555-***-****
```

#### `credit_card`
Shows only the last 4 digits of credit card numbers.

**Use cases:** Payment processing logs, transaction records
```bash
--mask "card_number:credit_card"
# 4111-1111-1111-1111 → ****-****-****-1111
```

#### `ssn`
Masks Social Security Numbers showing only last 4 digits.

**Use cases:** Identity verification systems
```bash
--mask "ssn:ssn"
# 123-45-6789 → ***-**-6789
```

### Partial Masking

These algorithms show only portions of the original data.

#### `partial`
Shows first and last N characters, masking the middle.

**Use cases:** Names, addresses, partial visibility
```bash
--mask "name:partial:2"
# "Jonathan" → "Jo****an"
```

#### `first_letter`
Shows only the first character.

**Use cases:** Initials, abbreviated names
```bash
--mask "first_name:first_letter"
# "Alice" → "A****"
```

#### `stars`
Replaces entire value with asterisks of the same length.

**Use cases:** Password fields, complete obfuscation
```bash
--mask "password:stars"
# "secret123" → "*********"
```

#### `fixed`
Replaces with a fixed value.

**Use cases:** Standardized replacement values
```bash
--mask "api_key:fixed:MASKED_KEY"
# "sk_live_abc123" → "MASKED_KEY"
```

### Tokenization

These algorithms replace values with tokens or identifiers.

#### `uuid`
Replaces with a UUID token. Same values get the same UUID (consistent).

**Use cases:** Creating surrogate keys, maintaining referential integrity
```bash
--mask "customer_id:uuid"
# "CUST001" → "550e8400-e29b-41d4-a716-446655440000"
```

#### `sequential`
Replaces with sequential integers starting from 1.

**Use cases:** Simple anonymization, reducing data size
```bash
--mask "account_number:sequential"
# "ACC-2024-001" → 1
# "ACC-2024-002" → 2
```

#### `random`
Replaces with random data of the same type.

**Use cases:** Test data generation, complete randomization
```bash
--mask "age:random"
# 35 → 67 (random number)
```

### Numeric Masking

These algorithms transform numeric values while preserving their general magnitude.

#### `round`
Rounds numbers to the nearest specified value.

**Use cases:** Salary bands, age groups, reducing precision
```bash
--mask "salary:round:5000"
# 52300 → 50000

--mask "age:round:10"
# 34 → 30
```

#### `range`
Replaces with a range bracket.

**Use cases:** Bucketing, categorical analysis
```bash
--mask "income:range:10000"
# 45000 → "40000-50000"

--mask "score:range:100"
# 234 → "200-300"
```

#### `noise`
Adds random noise to numeric values.

**Use cases:** Statistical privacy, differential privacy
```bash
--mask "revenue:noise:0.1"
# 100000 → 91234 (±10% random noise)

--mask "temperature:noise:0.05"
# 98.6 → 97.2 (±5% random noise)
```

### Date Masking

These algorithms transform date and datetime values.

#### `date_shift`
Adds or subtracts random days within a specified range.

**Use cases:** Preserving date relationships while obscuring exact dates
```bash
--mask "birth_date:date_shift:30"
# 1990-05-15 → 1990-06-02 (shifted ±30 days randomly)
```

#### `year_only`
Keeps only the year portion of dates.

**Use cases:** Age analysis, cohort studies
```bash
--mask "registration_date:year_only"
# 2024-03-15 → 2024
```

#### `month_year`
Keeps only month and year.

**Use cases:** Seasonal analysis, monthly aggregations
```bash
--mask "purchase_date:month_year"
# 2024-03-15 → "2024-03"
```

## Use Case Examples

### GDPR Compliance for Development Environment

```bash
ingestr ingest \
  --source-uri "postgres://prod_user:pass@prod.db/customers" \
  --source-table "customer_data" \
  --dest-uri "postgres://dev_user:pass@dev.db/customers" \
  --dest-table "customer_data" \
  --mask "email:hash" \
  --mask "phone:phone" \
  --mask "name:partial:1" \
  --mask "address:redact" \
  --mask "ip_address:hash" \
  --mask "birth_date:year_only"
```

### Healthcare Data for Analytics

```bash
ingestr ingest \
  --source-uri "mysql://user:pass@hospital.db/patients" \
  --source-table "patient_records" \
  --dest-uri "bigquery://project/dataset" \
  --dest-table "patient_analytics" \
  --mask "patient_id:uuid" \
  --mask "ssn:redact" \
  --mask "diagnosis_notes:redact" \
  --mask "admission_date:date_shift:7" \
  --mask "age:round:5"
```

### Financial Data for Testing

```bash
ingestr ingest \
  --source-uri "snowflake://account/database/schema" \
  --source-table "transactions" \
  --dest-uri "duckdb:///test_data.db" \
  --dest-table "test_transactions" \
  --mask "account_number:sequential" \
  --mask "card_number:credit_card" \
  --mask "amount:noise:0.2" \
  --mask "merchant_name:fixed:TEST_MERCHANT"
```

### E-commerce Data Sharing

```bash
ingestr ingest \
  --source-uri "postgres://internal.db/ecommerce" \
  --source-table "orders" \
  --dest-uri "s3://partner-bucket/data.parquet" \
  --dest-table "shared_orders" \
  --mask "customer_email:email" \
  --mask "shipping_address:first_letter" \
  --mask "order_value:round:10" \
  --mask "customer_name:partial:2"
```

## Best Practices

### Choosing the Right Algorithm

1. **For PII (Personally Identifiable Information)**
   - Use `hash` for consistent anonymization
   - Use `redact` for complete removal
   - Use format-preserving masks (`email`, `phone`, `ssn`) for maintaining data structure

2. **For Development/Testing**
   - Use `uuid` or `sequential` for maintaining relationships
   - Use `random` for generating test data
   - Use `partial` for semi-realistic data

3. **For Analytics**
   - Use `round` or `range` for numerical aggregations
   - Use `date_shift` for time-series analysis
   - Use `year_only` or `month_year` for temporal grouping

4. **For Compliance**
   - GDPR: Consider `hash`, `redact`, or `uuid` for personal data
   - HIPAA: Use `redact` for medical records, `date_shift` for dates
   - PCI DSS: Use `credit_card` for card numbers

### Performance Considerations

- **Hash-based algorithms** are fast and consistent
- **Random algorithms** have minimal overhead but don't preserve consistency
- **Format-preserving masks** have moderate performance impact
- **Multiple masks** can be applied efficiently in a single pass

### Security Notes

1. **Hashed values** are one-way transformations but may be vulnerable to rainbow table attacks for common values
2. **Partial masking** may not provide sufficient protection for highly sensitive data
3. **Date shifting** preserves intervals between dates, which may leak information
4. **Consistent tokenization** (uuid, hash) maintains relationships which could be exploited
5. Always validate that your masking strategy meets your compliance requirements

## Environment Variables

You can also set masking configurations via environment variables:

```bash
export INGESTR_MASK="email:hash,phone:partial:3,ssn:redact"
```

Multiple masks should be comma-separated when using environment variables.

## Limitations

- Masking is applied in-memory during the ingestion process
- The original source data remains unchanged
- Some algorithms require additional dependencies (e.g., `date_shift` requires `python-dateutil`)
- Masking adds processing overhead proportional to the data volume and number of masks applied
