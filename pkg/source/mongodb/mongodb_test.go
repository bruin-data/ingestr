package mongodb

import (
	"fmt"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/arrowutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestParseTableSpec(t *testing.T) {
	tests := []struct {
		name      string
		uriDB     string
		input     string
		wantDB    string
		wantCol   string
		wantQuery bool
		wantErr   bool
	}{
		{
			name:    "database.collection from table name",
			input:   "bugece.event",
			wantDB:  "bugece",
			wantCol: "event",
		},
		{
			name:    "plain collection without database errors (regular path)",
			input:   "movies",
			wantErr: true,
		},
		{
			name:    "multiple dots splits on first dot only",
			input:   "mydb.my.collection",
			wantDB:  "mydb",
			wantCol: "my.collection",
		},
		{
			name:      "custom query with database.collection",
			input:     `bugece.event:[{"$match":{"status":"active"}}]`,
			wantDB:    "bugece",
			wantCol:   "event",
			wantQuery: true,
		},
		{
			name:      "custom query without dot falls back to URI database",
			uriDB:     "mydb_from_uri",
			input:     `event:[{"$match":{"status":"active"}}]`,
			wantDB:    "mydb_from_uri",
			wantCol:   "event",
			wantQuery: true,
		},
		{
			name:    "custom query without dot and no URI database errors",
			uriDB:   "",
			input:   `event:[{"$match":{"status":"active"}}]`,
			wantErr: true,
		},
		{
			name:    "invalid query JSON",
			input:   `bugece.event:[not valid json`,
			wantErr: true,
		},
		{
			name:    "empty pipeline",
			input:   `bugece.event:[]`,
			wantErr: true,
		},
		{
			name:    "empty database part",
			input:   ".event",
			wantErr: true,
		},
		{
			name:    "empty collection part",
			input:   "bugece.",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &MongoDBSource{database: tt.uriDB}
			db, col, query, err := s.parseTableSpec(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if db != tt.wantDB {
				t.Errorf("database: got %q, want %q", db, tt.wantDB)
			}
			if col != tt.wantCol {
				t.Errorf("collection: got %q, want %q", col, tt.wantCol)
			}
			if tt.wantQuery && query == nil {
				t.Errorf("expected non-nil query")
			}
			if !tt.wantQuery && query != nil {
				t.Errorf("expected nil query, got %v", query)
			}
		})
	}
}

func TestValidateIncrementalKeyProjection(t *testing.T) {
	tests := []struct {
		name           string
		pipeline       []bson.M
		incrementalKey string
		wantErr        bool
	}{
		{
			name:           "inclusion projection with key present",
			pipeline:       []bson.M{{"$project": bson.M{"_id": 1, "name": 1, "created_at": 1}}},
			incrementalKey: "created_at",
		},
		{
			name:           "inclusion projection missing key",
			pipeline:       []bson.M{{"$project": bson.M{"_id": 1, "name": 1}}},
			incrementalKey: "created_at",
			wantErr:        true,
		},
		{
			name:           "exclusion projection (key included by default)",
			pipeline:       []bson.M{{"$project": bson.M{"password": float64(0)}}},
			incrementalKey: "created_at",
		},
		{
			name:           "no project stage",
			pipeline:       []bson.M{{"$match": bson.M{"status": "active"}}},
			incrementalKey: "created_at",
		},
		{
			name: "project stage after match with key present",
			pipeline: []bson.M{
				{"$match": bson.M{"status": "active"}},
				{"$project": bson.M{"status": 1, "created_at": 1}},
			},
			incrementalKey: "created_at",
		},
		{
			name: "project stage after match missing key",
			pipeline: []bson.M{
				{"$match": bson.M{"status": "active"}},
				{"$project": bson.M{"status": 1}},
			},
			incrementalKey: "created_at",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIncrementalKeyProjection(tt.pipeline, tt.incrementalKey)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestExtractDatabase(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{
			name: "standard mongodb URI",
			uri:  "mongodb://localhost:27017/mydb",
			want: "mydb",
		},
		{
			name: "mongodb+srv URI",
			uri:  "mongodb+srv://user:pass@cluster.example.net/sample_mflix?appName=Cluster0",
			want: "sample_mflix",
		},
		{
			name: "no database in URI",
			uri:  "mongodb://localhost:27017",
			want: "",
		},
		{
			name: "empty path",
			uri:  "mongodb://localhost:27017/",
			want: "",
		},
		{
			name: "URI with query params",
			uri:  "mongodb://localhost:27017/testdb?retryWrites=true",
			want: "testdb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDatabase(tt.uri)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSubstituteIntervalParams(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	t.Run("replaces both placeholders", func(t *testing.T) {
		pipeline := []bson.M{
			{"$match": bson.M{
				"created_at": bson.M{
					"$gte": ":interval_start",
					"$lt":  ":interval_end",
				},
			}},
		}

		result := substituteIntervalParams(pipeline, &start, &end)

		match := result[0]["$match"].(bson.M)
		createdAt := match["created_at"].(bson.M)

		startVal, ok := createdAt["$gte"].(primitive.DateTime)
		if !ok {
			t.Fatalf("expected primitive.DateTime for $gte, got %T", createdAt["$gte"])
		}
		if !startVal.Time().Equal(start) {
			t.Errorf("$gte: got %v, want %v", startVal.Time(), start)
		}

		endVal, ok := createdAt["$lt"].(primitive.DateTime)
		if !ok {
			t.Fatalf("expected primitive.DateTime for $lt, got %T", createdAt["$lt"])
		}
		if !endVal.Time().Equal(end) {
			t.Errorf("$lt: got %v, want %v", endVal.Time(), end)
		}
	})

	t.Run("nil intervals leaves placeholders unchanged", func(t *testing.T) {
		pipeline := []bson.M{
			{"$match": bson.M{"date": ":interval_start"}},
		}

		result := substituteIntervalParams(pipeline, nil, nil)
		match := result[0]["$match"].(bson.M)
		if match["date"] != ":interval_start" {
			t.Errorf("expected placeholder unchanged, got %v", match["date"])
		}
	})

	t.Run("nested arrays are traversed", func(t *testing.T) {
		pipeline := []bson.M{
			{"$match": bson.M{
				"$or": []any{
					bson.M{"start": ":interval_start"},
					bson.M{"end": ":interval_end"},
				},
			}},
		}

		result := substituteIntervalParams(pipeline, &start, &end)
		match := result[0]["$match"].(bson.M)
		or := match["$or"].([]any)

		startItem := or[0].(bson.M)
		if _, ok := startItem["start"].(primitive.DateTime); !ok {
			t.Errorf("expected primitive.DateTime in nested array, got %T", startItem["start"])
		}
	})

	t.Run("non-placeholder strings are untouched", func(t *testing.T) {
		pipeline := []bson.M{
			{"$match": bson.M{"status": "active"}},
		}

		result := substituteIntervalParams(pipeline, &start, &end)
		match := result[0]["$match"].(bson.M)
		if match["status"] != "active" {
			t.Errorf("expected 'active', got %v", match["status"])
		}
	})
}

func TestNormalizeBatchSize(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "default when unset", in: 0, want: defaultBatchSize},
		{name: "keep smaller batch", in: 5000, want: 5000},
		{name: "keep larger explicit batch", in: 25000, want: 25000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeBatchSize(tt.in)
			if got != tt.want {
				t.Fatalf("normalizeBatchSize(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestMongoBatchBuilder_AppendsAndBackfillsNulls(t *testing.T) {
	builder := newMongoBatchBuilder([]string{"skip"})

	if err := builder.AppendDocument(bson.M{"alpha": "one", "skip": "ignored"}); err != nil {
		t.Fatalf("AppendDocument() first doc error = %v", err)
	}
	if err := builder.AppendDocument(bson.M{"beta": int32(2)}); err != nil {
		t.Fatalf("AppendDocument() second doc error = %v", err)
	}

	record, err := builder.NewRecordBatch()
	if err != nil {
		t.Fatalf("NewRecordBatch() error = %v", err)
	}
	defer record.Release()

	if got := record.NumRows(); got != 2 {
		t.Fatalf("record.NumRows() = %d, want 2", got)
	}
	if got := record.NumCols(); got != 2 {
		t.Fatalf("record.NumCols() = %d, want 2", got)
	}

	if got := record.Schema().Field(0).Name; got != "alpha" {
		t.Fatalf("field 0 = %q, want alpha", got)
	}
	if got := record.Schema().Field(1).Name; got != "beta" {
		t.Fatalf("field 1 = %q, want beta", got)
	}

	if got := arrowutil.Value(record.Column(0), 0); got != "one" {
		t.Fatalf("alpha row 0 = %#v, want %q", got, "one")
	}
	if got := arrowutil.Value(record.Column(0), 1); got != nil {
		t.Fatalf("alpha row 1 = %#v, want nil", got)
	}
	if got := arrowutil.Value(record.Column(1), 0); got != nil {
		t.Fatalf("beta row 0 = %#v, want nil", got)
	}
	if got := arrowutil.Value(record.Column(1), 1); got != int64(2) {
		t.Fatalf("beta row 1 = %#v, want %v", got, int64(2))
	}
}

func TestMongoBatchBuilder_NewRecordBatchResetsBuilder(t *testing.T) {
	builder := newMongoBatchBuilder(nil)

	if err := builder.AppendDocument(bson.M{"beta": int32(2)}); err != nil {
		t.Fatalf("AppendDocument() first doc error = %v", err)
	}

	record, err := builder.NewRecordBatch()
	if err != nil {
		t.Fatalf("NewRecordBatch() error = %v", err)
	}
	record.Release()

	if builder.rowCount != 0 {
		t.Fatalf("builder.rowCount = %d, want 0", builder.rowCount)
	}
	if len(builder.fieldOrder) != 0 {
		t.Fatalf("len(builder.fieldOrder) = %d, want 0", len(builder.fieldOrder))
	}
	if len(builder.cols) != 0 {
		t.Fatalf("len(builder.cols) = %d, want 0", len(builder.cols))
	}

	if err := builder.AppendDocument(bson.M{"alpha": "one"}); err != nil {
		t.Fatalf("AppendDocument() after reset error = %v", err)
	}

	reusedRecord, err := builder.NewRecordBatch()
	if err != nil {
		t.Fatalf("NewRecordBatch() after reset error = %v", err)
	}
	defer reusedRecord.Release()

	if got := reusedRecord.NumRows(); got != 1 {
		t.Fatalf("reusedRecord.NumRows() = %d, want 1", got)
	}
	if got := reusedRecord.Schema().Field(0).Name; got != "alpha" {
		t.Fatalf("field 0 = %q, want alpha", got)
	}
}

func TestMongoBatchBuilder_NewRecordBatchSortsFields(t *testing.T) {
	builder := newMongoBatchBuilder(nil)

	if err := builder.AppendDocument(bson.M{"zeta": "one"}); err != nil {
		t.Fatalf("AppendDocument() first doc error = %v", err)
	}
	if err := builder.AppendDocument(bson.M{"alpha": int32(2)}); err != nil {
		t.Fatalf("AppendDocument() second doc error = %v", err)
	}

	record, err := builder.NewRecordBatch()
	if err != nil {
		t.Fatalf("NewRecordBatch() error = %v", err)
	}
	defer record.Release()

	if got := record.Schema().Field(0).Name; got != "alpha" {
		t.Fatalf("field 0 = %q, want alpha", got)
	}
	if got := record.Schema().Field(1).Name; got != "zeta" {
		t.Fatalf("field 1 = %q, want zeta", got)
	}
}

func TestMongoBatchBuilder_TypedColumnsByValue(t *testing.T) {
	oid, _ := primitive.ObjectIDFromHex("507f1f77bcf86cd799439011")
	ts := primitive.NewDateTimeFromTime(time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC))

	builder := newMongoBatchBuilder(nil)
	if err := builder.AppendDocument(bson.M{
		"id":      oid,
		"score":   3.14,
		"count":   int64(7),
		"active":  true,
		"created": ts,
		"name":    "alpha",
	}); err != nil {
		t.Fatalf("AppendDocument error = %v", err)
	}

	record, err := builder.NewRecordBatch()
	if err != nil {
		t.Fatalf("NewRecordBatch error = %v", err)
	}
	defer record.Release()

	want := map[string]string{
		"active":  "bool",
		"count":   "int64",
		"created": "timestamp[us, tz=UTC]",
		"id":      "utf8",
		"name":    "utf8",
		"score":   "float64",
	}
	for i := 0; i < int(record.NumCols()); i++ {
		field := record.Schema().Field(i)
		if got, ok := want[field.Name]; !ok {
			t.Errorf("unexpected column %q", field.Name)
		} else if field.Type.String() != got {
			t.Errorf("%s: type = %s, want %s", field.Name, field.Type, got)
		}
	}
}

func TestMongoBatchBuilder_PromotesOnTypeMismatch(t *testing.T) {
	builder := newMongoBatchBuilder(nil)

	// First value picks Int64 type, second value (string) forces promotion to unknown.
	if err := builder.AppendDocument(bson.M{"x": int32(42)}); err != nil {
		t.Fatalf("AppendDocument 1 error = %v", err)
	}
	if err := builder.AppendDocument(bson.M{"x": "hello"}); err != nil {
		t.Fatalf("AppendDocument 2 error = %v", err)
	}

	record, err := builder.NewRecordBatch()
	if err != nil {
		t.Fatalf("NewRecordBatch error = %v", err)
	}
	defer record.Release()

	field := record.Schema().Field(0)
	if field.Name != "x" {
		t.Fatalf("field 0 name = %q, want x", field.Name)
	}
	// Promoted to the unknown extension type so that mixed values can be JSON-encoded.
	if !isUnknownType(field.Type) {
		t.Fatalf("expected promoted column to use unknown type, got %s", field.Type)
	}
}

func TestMongoBatchBuilder_PromotesPreservesEarlierValues(t *testing.T) {
	builder := newMongoBatchBuilder(nil)

	// 50 ints then 50 strings in the same batch. The promotion path must
	// re-encode the earlier int values as JSON strings so no data is lost.
	for i := range 50 {
		if err := builder.AppendDocument(bson.M{"v": int64(i)}); err != nil {
			t.Fatalf("AppendDocument int #%d error = %v", i, err)
		}
	}
	for i := 50; i < 100; i++ {
		if err := builder.AppendDocument(bson.M{"v": fmt.Sprintf("str_%d", i)}); err != nil {
			t.Fatalf("AppendDocument str #%d error = %v", i, err)
		}
	}

	record, err := builder.NewRecordBatch()
	if err != nil {
		t.Fatalf("NewRecordBatch error = %v", err)
	}
	defer record.Release()

	if got := record.NumRows(); got != 100 {
		t.Fatalf("NumRows = %d, want 100", got)
	}
	if !isUnknownType(record.Schema().Field(0).Type) {
		t.Fatalf("expected unknown type after promotion, got %s", record.Schema().Field(0).Type)
	}

	// Earlier int values are JSON-encoded as their decimal representation.
	if got := arrowutil.Value(record.Column(0), 0); got != "0" {
		t.Errorf("row 0 = %#v, want %q", got, "0")
	}
	if got := arrowutil.Value(record.Column(0), 49); got != "49" {
		t.Errorf("row 49 = %#v, want %q", got, "49")
	}
	// Later string values are JSON-encoded with quotes.
	if got := arrowutil.Value(record.Column(0), 50); got != `"str_50"` {
		t.Errorf("row 50 = %#v, want %q", got, `"str_50"`)
	}
	if got := arrowutil.Value(record.Column(0), 99); got != `"str_99"` {
		t.Errorf("row 99 = %#v, want %q", got, `"str_99"`)
	}
}

func TestMongoBatchBuilder_NewColumnMidBatchBackfillsNulls(t *testing.T) {
	builder := newMongoBatchBuilder(nil)

	for i := range 5 {
		if err := builder.AppendDocument(bson.M{"a": int64(i)}); err != nil {
			t.Fatalf("doc %d error = %v", i, err)
		}
	}
	// New column "b" appears at row 5; previous five rows must be back-filled with nulls.
	if err := builder.AppendDocument(bson.M{"a": int64(5), "b": "hello"}); err != nil {
		t.Fatalf("doc with new col error = %v", err)
	}

	record, err := builder.NewRecordBatch()
	if err != nil {
		t.Fatalf("NewRecordBatch error = %v", err)
	}
	defer record.Release()

	bIdx := -1
	for i := 0; i < int(record.NumCols()); i++ {
		if record.Schema().Field(i).Name == "b" {
			bIdx = i
		}
	}
	if bIdx < 0 {
		t.Fatalf("column b not found")
	}
	col := record.Column(bIdx)
	for i := range 5 {
		if !col.IsNull(i) {
			t.Errorf("b row %d expected null, got %v", i, arrowutil.Value(col, i))
		}
	}
	if got := arrowutil.Value(col, 5); got != "hello" {
		t.Errorf("b row 5 = %#v, want %q", got, "hello")
	}
}

func TestMongoBatchBuilder_AllNullColumnEmittedAsUnknown(t *testing.T) {
	// A column that only ever sees nulls must be emitted as the unknown
	// extension type so the schema inferrer can drop it (the existing
	// drop-empty-columns behavior).
	builder := newMongoBatchBuilder(nil)
	for i := range 3 {
		if err := builder.AppendDocument(bson.M{"a": int64(i), "b": nil}); err != nil {
			t.Fatalf("doc %d error = %v", i, err)
		}
	}

	record, err := builder.NewRecordBatch()
	if err != nil {
		t.Fatalf("NewRecordBatch error = %v", err)
	}
	defer record.Release()

	for i := 0; i < int(record.NumCols()); i++ {
		field := record.Schema().Field(i)
		if field.Name != "b" {
			continue
		}
		if !isUnknownType(field.Type) {
			t.Fatalf("all-null column b: type = %s, want unknown", field.Type)
		}
	}
}

func TestMongoBatchBuilder_NumericPromotionWithinBatch(t *testing.T) {
	// First value is float64 → Float64Builder. A subsequent int64 must be
	// upcast into the same Float64 column without promoting to unknown.
	builder := newMongoBatchBuilder(nil)
	if err := builder.AppendDocument(bson.M{"v": 3.14}); err != nil {
		t.Fatalf("doc 1 error = %v", err)
	}
	if err := builder.AppendDocument(bson.M{"v": int64(7)}); err != nil {
		t.Fatalf("doc 2 error = %v", err)
	}

	record, err := builder.NewRecordBatch()
	if err != nil {
		t.Fatalf("NewRecordBatch error = %v", err)
	}
	defer record.Release()

	if got := record.Schema().Field(0).Type.String(); got != "float64" {
		t.Fatalf("type = %s, want float64", got)
	}
	if got := arrowutil.Value(record.Column(0), 0); got != 3.14 {
		t.Errorf("row 0 = %#v, want 3.14", got)
	}
	if got := arrowutil.Value(record.Column(0), 1); got != 7.0 {
		t.Errorf("row 1 = %#v, want 7.0", got)
	}
}

func TestMongoBatchBuilder_NestedDocAndArrayUseUnknownPath(t *testing.T) {
	// Nested documents and arrays are not directly representable as a typed
	// scalar column, so the typed builder must fall through to the unknown
	// extension type and JSON-encode the value.
	builder := newMongoBatchBuilder(nil)
	doc := bson.M{
		"meta":   bson.M{"src": "test", "level": int64(1)},
		"tags":   primitive.A{"a", "b", "c"},
		"scalar": "ok",
	}
	if err := builder.AppendDocument(doc); err != nil {
		t.Fatalf("AppendDocument error = %v", err)
	}

	record, err := builder.NewRecordBatch()
	if err != nil {
		t.Fatalf("NewRecordBatch error = %v", err)
	}
	defer record.Release()

	for i := 0; i < int(record.NumCols()); i++ {
		field := record.Schema().Field(i)
		switch field.Name {
		case "meta", "tags":
			if !isUnknownType(field.Type) {
				t.Errorf("%s: type = %s, want unknown", field.Name, field.Type)
			}
		case "scalar":
			if field.Type.String() != "utf8" {
				t.Errorf("scalar: type = %s, want utf8", field.Type)
			}
		}
	}
}

func TestConvertMongoShellToExtendedJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "ObjectId",
			input: `[{"$match": {"_id": ObjectId("507f1f77bcf86cd799439011")}}]`,
			want:  `[{"$match": {"_id": {"$oid": "507f1f77bcf86cd799439011"}}}]`,
		},
		{
			name:  "ISODate",
			input: `[{"$match": {"created_at": {"$gte": ISODate("2025-01-01T00:00:00Z")}}}]`,
			want:  `[{"$match": {"created_at": {"$gte": {"$date": "2025-01-01T00:00:00Z"}}}}]`,
		},
		{
			name:  "NumberLong with quotes",
			input: `[{"$match": {"count": NumberLong("12345678901234")}}]`,
			want:  `[{"$match": {"count": {"$numberLong": "12345678901234"}}}]`,
		},
		{
			name:  "NumberLong without quotes",
			input: `[{"$match": {"count": NumberLong(42)}}]`,
			want:  `[{"$match": {"count": {"$numberLong": "42"}}}]`,
		},
		{
			name:  "NumberInt",
			input: `[{"$match": {"age": NumberInt(25)}}]`,
			want:  `[{"$match": {"age": {"$numberInt": "25"}}}]`,
		},
		{
			name:  "NumberDecimal",
			input: `[{"$match": {"price": NumberDecimal("19.99")}}]`,
			want:  `[{"$match": {"price": {"$numberDecimal": "19.99"}}}]`,
		},
		{
			name:  "Timestamp",
			input: `[{"$match": {"ts": Timestamp(1234, 1)}}]`,
			want:  `[{"$match": {"ts": {"$timestamp": {"t": 1234, "i": 1}}}}]`,
		},
		{
			name:  "MinKey and MaxKey",
			input: `[{"$match": {"$gte": MinKey(), "$lte": MaxKey()}}]`,
			want:  `[{"$match": {"$gte": {"$minKey": 1}, "$lte": {"$maxKey": 1}}}]`,
		},
		{
			name:  "UUID",
			input: `[{"$match": {"uid": UUID("550e8400-e29b-41d4-a716-446655440000")}}]`,
			want:  `[{"$match": {"uid": {"$uuid": "550e8400-e29b-41d4-a716-446655440000"}}}]`,
		},
		{
			name:  "plain JSON unchanged",
			input: `[{"$match": {"status": "active"}}]`,
			want:  `[{"$match": {"status": "active"}}]`,
		},
		{
			name:  "multiple constructors in one pipeline",
			input: `[{"$match": {"_id": ObjectId("abc"), "date": ISODate("2025-01-01")}}]`,
			want:  `[{"$match": {"_id": {"$oid": "abc"}, "date": {"$date": "2025-01-01"}}}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertMongoShellToExtendedJSON(tt.input)
			if got != tt.want {
				t.Errorf("\ngot:  %s\nwant: %s", got, tt.want)
			}
		})
	}
}
