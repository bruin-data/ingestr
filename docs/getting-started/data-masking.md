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