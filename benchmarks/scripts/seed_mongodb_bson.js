const databaseName = seedDatabase;
const collectionName = seedCollection;
const rowCount = seedRows;
const batchSize = typeof seedBatchSize === "undefined" ? 10000 : seedBatchSize;

if (!databaseName || !collectionName || !Number.isInteger(rowCount) || rowCount < 0) {
  throw new Error("seedDatabase, seedCollection, and non-negative integer seedRows are required");
}

const targetDB = db.getSiblingDB(databaseName);
const collection = targetDB.getCollection(collectionName);

collection.drop();

function objectIdFor(row) {
  return ObjectId(row.toString(16).padStart(24, "0"));
}

function baseDateFor(row) {
  return new Date(Date.UTC(2020, 0, 1, 0, 0, 0) + (row % 1500) * 86400000);
}

function timestampFor(row) {
  return Timestamp(1577836800 + (row % 31536000), row % 1000);
}

function documentFor(row) {
  const id = NumberInt(row);
  const date = baseDateFor(row);
  const tags = ["tag_" + (row % 10), "bucket_" + (row % 100)];

  const doc = {
    _id: objectIdFor(row),
    id,
    small_str: "name_" + (row % 10000),
    medium_str: "user_" + row + "@example-" + (row % 500) + ".com",
    large_str: String.fromCharCode(65 + (row % 26)).repeat(50 + (row % 200)),
    tiny_int: NumberInt(row % 32767),
    regular_int: id,
    big_int: NumberLong((BigInt(row) * 1000000n).toString()),
    float_val: row / 7.0 + (row % 1000),
    decimal_val: NumberDecimal(((row % 1000000) / 100).toFixed(4)),
    bool_val: row % 2 === 0,
    date_val: date,
    ts_val: new Date(Date.UTC(2020, 0, 1, 0, 0, row % 60, row % 1000)),
    ts_tz_val: new Date(Date.UTC(2020, 0, 1, 3, 0, row % 60, row % 1000)),
    extra_text: "extra_text_row_" + row + "_" + "x".repeat(50 + (row % 100)),
    object_id_val: objectIdFor(rowCount + row),
    decimal128_val: NumberDecimal(row + "." + String(row % 10000).padStart(4, "0")),
    nested_doc: {
      profile: {
        score: row / 13.0,
        active: row % 3 !== 0,
        updated_at: date,
      },
      labels: tags,
    },
    array_val: [id, "item_" + (row % 50), { rank: NumberInt(row % 1000) }],
    binary_val: BinData(0, "AQIDBAUGBwg="),
    regex_val: new RegExp("^name_" + (row % 10000), "i"),
    timestamp_val: timestampFor(row),
    null_val: null,
  };

  if (row % 2 === 0) {
    doc.optional_val = "present_" + row;
  }

  return doc;
}

let batch = [];
for (let row = 1; row <= rowCount; row++) {
  batch.push(documentFor(row));
  if (batch.length >= batchSize) {
    collection.insertMany(batch, { ordered: false });
    batch = [];
  }
}

if (batch.length > 0) {
  collection.insertMany(batch, { ordered: false });
}

collection.createIndex({ id: 1 });
collection.createIndex({ date_val: 1 });

print("seeded " + collection.countDocuments() + " BSON documents into " + databaseName + "." + collectionName);
