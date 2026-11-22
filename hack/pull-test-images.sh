#!/usr/bin/env bash
# Starts background pulls of the testcontainer images `make test-integration`
# uses, so they download while Go compiles. Shared by tests.yml and release.yml.

set -euo pipefail

images=(
	mongo:7
	postgres:16-alpine
	postgres:17-alpine
	mysql:8.0
	mcr.microsoft.com/mssql/server:2022-latest
	clickhouse/clickhouse-server:24.3
	amazon/dynamodb-local:latest
	crate:latest
	pgsty/minio:RELEASE.2026-08-04T00-00-00Z
	rabbitmq:3-management-alpine
	starrocks/allin1-ubuntu:3.3-latest
	gvenzl/oracle-free:23-slim
	vitess/vttestserver:mysql80
	redpandadata/redpanda:v24.2.7
	localstack/localstack:3.8.1
	cassandra:4.1
	maxcompute/maxcompute-emulator:v0.0.7
	docker.elastic.co/elasticsearch/elasticsearch:9.4.4
	mcr.microsoft.com/azure-storage/azurite:3.35.0
	apache/iceberg-rest-fixture:1.9.2
	gcr.io/cloud-spanner-emulator/emulator:latest
	gcr.io/google.com/cloudsdktool/google-cloud-cli:emulators
	apachepulsar/pulsar:3.3.0
	nats:2.10-alpine
	redis:7-alpine
	couchdb:3.4.3
	eclipse-mosquitto:2.0
	trinodb/trino:483
	ghcr.io/trinodb/testing/hive3.1:129
	ghcr.io/projectnessie/nessie:0.104.5
)

for img in "${images[@]}"; do
	docker pull "$img" >/dev/null 2>&1 &
done
