# **Frankfurter Source Documentation**

The `frankfurter` source in the `ingestr` pipeline is designed to fetch exchange rate data from the [Frankfurter API](https://www.frankfurter.dev/). This source supports fetching the latest exchange rates, historical exchange rates, and currency metadata. The data can be ingested into a specified destination database, such as DuckDB.

---

## **Command Overview**

The `ingestr` command to use the `frankfurter` source is as follows:

```bash
ingestr ingest \
--source-uri 'frankfurter://?base=IDR' \
--interval-start '2025-03-27' \     # Optional. See 'exchange_rates'.
--interval-end '2025-03-28' \       # Optional.
--source-table '<table_name>' \     # E.g 'currencies', 'latest', 'exchange_rates'. See below.
--dest-uri '<your-destination-uri>' \
--dest-table '<your-schema>.<your-table_name>'
```

---

## **Command Parameters**

### **`--source-uri`**
- **Description**: Specifies the source URI for the Frankfurter API.
- **Value**: `'frankfurter://'`
- **Purpose**: Indicates that the data will be fetched from the Frankfurter API. 
  - An optional base currency can be added `?base={base_currency}`.
  - If no base currency is included, base currency defaults to USD.

---

### **`--interval-start` (Optional)**
- **Description**: The start date for fetching historical exchange rates.
- **Value**: A date in the format `YYYY-MM-DD` (e.g., `'2025-03-27'`).
- **Purpose**: Defines the starting point for fetching historical data.
  - For `latest` and `currencies` this parameter is ignored.
  - For `exchange_rates`, it defaults to the current date if not provided.

---

### **`--interval-end` (Optional)**
- **Description**: The end date for fetching historical exchange rates.
- **Value**: A date in the format `YYYY-MM-DD` (e.g., `'2025-03-28'`).
- **Purpose**: Defines the end point for fetching historical data. 
    - If `--interval-start` is provided without `--interval-end`, `--interval-end` defaults to the current date and retrieves up until the latest published data.
    - If `--interval-end` is provided without `--interval-start`, it will be ignored and the call will retrieve the last published data.
    - For `latest` and `currencies` this parameter is ignored.

---

### **`--source-table`**
- **Description**: Specifies the table to fetch data from.
- **Value**: One of the following:
  - **`currencies`**: Fetches a list of the available currencies and their ISO 4217 currency code (e.g. `USD`, `EUR`, `GBP`).
  - **`latest`**: Fetches the latest exchange rates.
  - **`exchange_rates`**: Fetches historical exchange rates for a specified date range.
- **Purpose**: Determines the type of data to fetch from the Frankfurter API.

---

### **`--dest-uri`**
- **Description**: Specifies the destination database URI.
- **Value**: The path to the database file (e.g., `'duckdb.db'`).
- **Purpose**: Defines where the fetched data will be stored.

---

### **`--dest-table`**
- **Description**: Specifies the destination table name.
- **Value**: A string in the format `{schema}.{table_name}` (e.g., `'schema.my_table'`).
- **Purpose**: Defines the schema and table name where the data will be written.
- **Notes**:
    - If the destination table does not yet exist in your database, ingestr will automatically create the table with name that is provided in this argument. The table will be structured according to the source table (see [Core Concepts](https://bruin-data.github.io/ingestr/getting-started/core-concepts.html)).

---

## **Supported Source Tables**

### **`currencies`**
- **Description**: Fetches a list of available currencies.
- **Columns**:
  - `currency_code`: The ISO 4217 currency code (e.g., `USD`, `EUR`).
  - `currency_name`: The name of the currency (e.g., `US Dollar`, `Euro`).
- **Primary Key**: `currency_code`

---

### **`latest`**
- **Description**: Fetches the latest exchange rates.
- **Columns**:
  - `date`: The date of the exchange rates.
  - `currency_code`: The ISO 4217 currency code (e.g., `USD`, `EUR`).
  - `rate`: The exchange rate relative to the base currency.
  - `base_currency`: The base currency used to calculate the exchange rate.
- **Primary Key**: Composite key of `date`, `currency_code` and `base_currency`.
- **Notes**:
  - The base currency (e.g., `EUR`) is included with a rate of `1.0`.

---

### **`exchange_rates`**
- **Description**: Fetches historical exchange rates for a specified date range.
- **Columns**:
  - `date`: The date of the exchange rates.
  - `currency_code`: The ISO 4217 currency code (e.g., `USD`, `EUR`).
  - `rate`: The exchange rate relative to the base currency.
  - `base_currency`: The base currency used to calculate the exchange rate.
- **Primary Key**: Composite key of `date`, `currency_code` and `base_currency`.
- **Notes**:
  - An optional start and end date can be added via the arguments `--interval-start` and optionally `--interval-end` to define the date range (see examples below). If no start date is specified, the date will default today's date (and thus return the latest exchange rates).
  - If a start date but no end date is specified, then the end date will default to today's date and ingestr will retrieve data up until the latest published data.
  - Note that the [Frankfurter API](https://www.frankfurter.dev/) only publishes updates Monday-Friday. If the given date is on the weekend, the date will default to the previous Friday. The source is however implemented in ingestr in such a way as to avoid duplicating rows of data in this case (see [Incremental Loading - Replace](https://bruin-data.github.io/ingestr/getting-started/incremental-loading.html)).

#### **Example Table: Handling Weekend Dates**
Here `--interval-start` is set to a weekend date (e.g., `2025-03-29` -- a Saturday). `--interval-end` is set to a the following Monday (`2025-03-31`). 

`--interval-start` defaults to the previous Friday (`2025-03-28`) and the next data is from the following Monday (for simplicity, only a subset of currencies is shown below):

| **date**     | **currency_code** | **rate** | **base_currency** |
|--------------|-------------------|----------|-------------------|
| 2025-03-28   | EUR               | 1.0      | EUR               |
| 2025-03-28   | USD               | 1.0783   | EUR               |
| 2025-03-28   | GBP               | 0.8571   | EUR               |
| 2025-03-31   | EUR               | 1.0      | EUR               |
| 2025-03-31   | USD               | 1.0783   | EUR               |
| 2025-03-31   | GBP               | 0.8571   | EUR               |


---

## **Examples**

### **1. Fetch the Latest Exchange Rates with GBP as Base Currency**
```bash
ingestr ingest \
--source-uri 'frankfurter://?base=GBP' \
--source-table 'latest' \
--dest-uri 'duckdb.db' \
--dest-table 'schema.latest_new_scheme'
```

---

### **2. Fetch Historical Exchange Rates with USD as Default Base Currency**
```bash
ingestr ingest \
--source-uri 'frankfurter://' \
--interval-start '2025-03-01' \
--interval-end '2025-03-10' \
--source-table 'exchange_rates' \
--dest-uri 'duckdb.db' \
--dest-table 'schema.exchange_rates'
```

---

### **3. Fetch Currency Metadata**
```bash
ingestr ingest \
--source-uri 'frankfurter://' \
--source-table 'currencies' \
--dest-uri 'duckdb.db' \
--dest-table 'schema.currencies'
```

---

## **Notes**
- Ensure that the destination database (`--dest-uri`) is accessible and writable.
- The `--interval-start` and `--interval-end` parameters are only applicable for the `exchange_rates` table.
- The `latest` table always fetches the most recent exchange rates and ignores date parameters.
