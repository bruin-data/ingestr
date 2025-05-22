# Frankfurter

[Frankfurter API](https://www.frankfurter.dev/) is an online platform which fetches current and historical exchnge rate data.

ingestr supports Frankfurter as a source primarily to demonstrate ingestr's features since the API doesn't require any authentication. 

## URI format

The URI format for Frankfurter is as follows:

```plaintext
frankfurter://?base=<currency-code-here>
```

URI parameters:
- `base`: defines the base currency code (e.g. EUR, USD, IDR) used to calculate the exchange rates. 


## Set-Up Frankfurter Integration

Let's say you want to fetch the exchange rates for a certain period with the base currency as Indian Rupees. Here's a sample command that will copy this data into your DuckDB database:

```bash
ingestr ingest \
--source-uri 'frankfurter://?base=INR' \
--interval-start '2025-03-20' \ 
--interval-end '2025-03-28' \       
--source-table 'exchange_rates' \    
--dest-uri 'duckdb///frankfurter.duckdb' \
--dest-table 'my_schema.exchange_rates'
```

The result of this command will be a list of currency exchange rates from 20.03.2025-28.03.2025 with INR as the base currency in your DuckDB database. 

## Tables

- `latest`: Fetches the latest exchange rates.
- `exchange_rates`: Fetches historical exchange rates for a specified date range.
- `currencies`: Fetches a list of the available currencies and their ISO 4217 currency code (e.g. `USD`, `EUR`, `GBP`).

Use these as `--source-table` parameter in the `ingestr ingest` command.


**Notes**:
- The arguments `--interval-start` and `--interval-end` are only relevant for the table exchange_rates.
- If a start date but no end date is specified, then the end date will default to today's date and ingestr will retrieve data up until the latest published data.
- Note that the [Frankfurter API](https://www.frankfurter.dev/) only publishes updates Monday-Friday. If the given date is on the weekend, the date will default to the previous Friday.

