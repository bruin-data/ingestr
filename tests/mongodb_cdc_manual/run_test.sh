#!/usr/bin/env bash
set -euo pipefail

MONGO_CONTAINER="${MONGO_CONTAINER:-ingestr_mongodb_cdc}"
MONGO_PORT="${MONGO_PORT:-27018}"
DB_NAME="${DB_NAME:-cdc_manual}"
SQLITE_PATH="${SQLITE_PATH:-/tmp/ingestr_mongodb_cdc_manual.db}"
SOURCE_URI="mongodb+cdc://localhost:${MONGO_PORT}/${DB_NAME}?directConnection=true&replicaSet=rs0&mode=batch&max_await_time=1s"
DEST_URI="sqlite:///${SQLITE_PATH}"

cleanup() {
  docker rm -f "${MONGO_CONTAINER}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

cleanup
rm -f "${SQLITE_PATH}"

docker run -d --name "${MONGO_CONTAINER}" -p "${MONGO_PORT}:27017" mongo:7 --replSet rs0 --bind_ip_all >/dev/null

until docker exec "${MONGO_CONTAINER}" mongosh --quiet --eval 'db.adminCommand("ping").ok' >/dev/null 2>&1; do
  sleep 1
done

docker exec "${MONGO_CONTAINER}" mongosh --quiet --eval 'rs.initiate({_id:"rs0",members:[{_id:0,host:"localhost:27017"}]})' >/dev/null || true
until docker exec "${MONGO_CONTAINER}" mongosh --quiet --eval 'db.hello().isWritablePrimary' | grep -q true; do
  sleep 1
done

docker exec "${MONGO_CONTAINER}" mongosh --quiet "${DB_NAME}" --eval '
db.items.drop();
db.items.insertMany([
  {_id: NumberLong(1), name: "item1", value: NumberLong(100)},
  {_id: NumberLong(2), name: "item2", value: NumberLong(200)},
  {_id: NumberLong(3), name: "item3", value: NumberLong(300)}
]);
' >/dev/null

make build

./bin/ingestr ingest \
  --source-uri "${SOURCE_URI}" \
  --source-table "${DB_NAME}.items" \
  --dest-uri "${DEST_URI}" \
  --dest-table items_dest \
  --yes

docker exec "${MONGO_CONTAINER}" mongosh --quiet "${DB_NAME}" --eval '
db.items.insertOne({_id: NumberLong(4), name: "item4", value: NumberLong(400)});
db.items.updateOne({_id: NumberLong(1)}, {$set: {value: NumberLong(150)}});
db.items.deleteOne({_id: NumberLong(2)});
db.items.updateOne({_id: NumberLong(3)}, {$set: {name: "item3_final", value: NumberLong(999)}});
db.items.deleteOne({_id: NumberLong(3)});
' >/dev/null

./bin/ingestr ingest \
  --source-uri "${SOURCE_URI}" \
  --source-table "${DB_NAME}.items" \
  --dest-uri "${DEST_URI}" \
  --dest-table items_dest \
  --yes

sqlite3 "${SQLITE_PATH}" <<'SQL'
.headers on
.mode column
SELECT "_id", name, value, "_cdc_deleted", "_cdc_lsn" FROM items_dest ORDER BY "_id";
SELECT
  COUNT(*) AS total_rows,
  SUM(CASE WHEN "_cdc_deleted" = 0 THEN 1 ELSE 0 END) AS active_rows,
  SUM(CASE WHEN "_cdc_deleted" = 1 THEN 1 ELSE 0 END) AS deleted_rows
FROM items_dest;
SQL

echo "Manual MongoDB CDC test complete: ${SQLITE_PATH}"
