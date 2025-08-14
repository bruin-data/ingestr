# Internet Society Pulse

[Internet Society Pulse](https://pulse.internetsociety.org/) is a platform that monitors the health, availability, and evolution of the Internet, providing metrics on key technologies that contribute to its security, resilience, and trustworthiness.

ingestr supports Internet Society Pulse as a source.

## URI format

The URI format for Internet Society Pulse is as follows:

```plaintext
isoc-pulse://?token=<your-token>
```

URI parameters:
- `token`: The API token used for authentication with the Internet Society Pulse API.

## Setting up an Internet Society Pulse Integration

To use the Internet Society Pulse source, you need to obtain an API token from the Internet Society.

Once you have the token, you can access various metrics from the Pulse platform. Here's a sample command that will copy data from the ISOC Pulse API into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'isoc-pulse://?token=your_token_here' \
  --source-table 'https' \
  --dest-uri 'duckdb:///pulse_data.duckdb' \
  --dest-table 'dest.https_adoption' \
  --interval-start '2023-01-01' \
  --interval-end '2023-12-31'
```

This will retrieve HTTPS adoption metrics for the specified date range and store them in your database.

## Tables

Internet Society Pulse source allows ingesting the following metrics as separate tables:

| Metric | Description | Country Support | Additional Options | PK | Inc Key | Inc Strategy |
|--------|-------------|-----------------|-------------------|-----|----------|--------------|
| `dnssec_adoption` | DNSSEC adoption metrics for specific domains | No | Domain name | date | date | merge |
| `dnssec_tld_adoption` | DNSSEC adoption metrics for top-level domains | Yes | Country code | date | date | merge |
| `dnssec_validation` | DNSSEC validation metrics | Yes | Country code | date | date | merge |
| `http` | HTTP protocol metrics | No | None | date | date | merge |
| `http3` | HTTP/3 protocol metrics | No | None | date | date | merge |
| `https` | HTTPS adoption metrics | Yes | topsites, Country code | date | date | merge |
| `ipv6` | IPv6 adoption metrics | Yes | topsites, Country code | date | date | merge |
| `net_loss` | Internet disconnection metrics | Yes | Shutdown type, Country code | date | date | merge |
| `resilience` | Internet resilience metrics | Yes | Country code | date | date | merge |
| `roa` | Route Origin Authorization metrics | Yes | IP version (4/6), Country code | date | date | merge |
| `rov` | Route Origin Validation metrics | No | None | date | date | merge |
| `tls` | TLS protocol metrics | No | None | date | date | merge |
| `tls13` | TLS 1.3 protocol metrics | No | None | date | date | merge |


Use these as `--source-table` parameter in the `ingestr ingest` command.

## Parameter Syntax

Many metrics support additional parameters, including country-specific data. These parameters are specified using colons in the source table name. The general format is:

```
metric[:option][:country]
```

Where the options and parameters vary by metric type.

### General Parameter Rules

1. Country codes should follow the ISO 3166-1 alpha-2 format (e.g., US, GB, DE)
2. The parameters are position-specific – the order matters

## Metric-Specific Parameters

### HTTPS Metrics

- Global data: `https`
- Country-specific: `https:US` 
- Top sites data: `https:topsites`
- Top sites for a specific country: `https:topsites:US`

### IPv6 Metrics

- Global data: `ipv6` 
- Country-specific: `ipv6:DE` 
- Top sites data: `ipv6:topsites`

### DNSSEC Metrics

- Global validation: `dnssec_validation`
- Country-specific validation: `dnssec_validation:SE` 
- Global TLD adoption: `dnssec_tld_adoption`
- Country-specific TLD adoption: `dnssec_tld_adoption:JP`

### ROA Metrics (Route Origin Authorization)

- Global for IPv4: `roa:4`
- Global for IPv6: `roa:6`
- Country-specific for IPv4: `roa:4:CN`
- Country-specific for IPv6: `roa:6:BR`

### Net Loss Metrics (Internet Disconnections)

- Country-specific with shutdown type: `net_loss:shutdown_type:country_code`
- Example for shutdowns in India: `net_loss:shutdown:IN`
- Example for internet blocking in Japan: `net_loss:blocking:JP`

### Resilience Metrics

- Global resilience: `resilience`
- Country-specific resilience: `resilience::FR`

## Examples

### Country-specific HTTPS Adoption

```sh
ingestr ingest \
  --source-uri 'isoc-pulse://?token=your_token_here' \
  --source-table 'https::US' \
  --dest-uri 'duckdb:///pulse_data.duckdb' \
  --dest-table 'dest.us_https_adoption' \
  --interval-start '2023-01-01'
```

### IPv6 Top Sites Data

```sh
ingestr ingest \
  --source-uri 'isoc-pulse://?token=your_token_here' \
  --source-table 'ipv6:topsites' \
  --dest-uri 'duckdb:///pulse_data.duckdb' \
  --dest-table 'dest.ipv6_topsites' \
  --interval-start '2023-01-01'
```

### ROA IPv4 Data by Country

```sh
ingestr ingest \
  --source-uri 'isoc-pulse://?token=your_token_here' \
  --source-table 'roa:4:US' \
  --dest-uri 'duckdb:///pulse_data.duckdb' \
  --dest-table 'dest.us_roa_ipv4' \
  --interval-start '2023-01-01'
```

## Further Reading:
* [Pulse Documentation](https://pulse.internetsociety.org/api/docs)