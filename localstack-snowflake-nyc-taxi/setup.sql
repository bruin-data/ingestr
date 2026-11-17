-- Run once against the emulator before the first `bruin run`.
-- The login needs the warehouse and database. Creating the medallion schemas up front,
-- plus ingestr's _BRUIN_STAGING, keeps parallel assets from racing to create them.
CREATE WAREHOUSE IF NOT EXISTS COMPUTE_WH;
CREATE DATABASE IF NOT EXISTS LOCALSTACK_DEMO;
CREATE SCHEMA IF NOT EXISTS LOCALSTACK_DEMO.BRONZE;
CREATE SCHEMA IF NOT EXISTS LOCALSTACK_DEMO.SILVER;
CREATE SCHEMA IF NOT EXISTS LOCALSTACK_DEMO.GOLD;
CREATE SCHEMA IF NOT EXISTS LOCALSTACK_DEMO._BRUIN_STAGING;
