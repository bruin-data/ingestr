package transformer

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/araddon/dateparse"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/google/uuid"
)

type maskSpec struct {
	column    string
	algorithm string
	param     string
	hasParam  bool
}

type algorithmDef struct {
	apply      func(*ColumnMasker, string, arrow.DataType, string, bool) (any, bool, error)
	outputType func(arrow.DataType) arrow.DataType
}

type ColumnMasker struct {
	specs map[string]maskSpec // column name (case-sensitive) -> spec

	mu        sync.Mutex
	uuidCache map[string]string
	seqCache  map[string]int64
	seqNext   int64
}

func NewColumnMasker(configs []string) (*ColumnMasker, error) {
	m := &ColumnMasker{
		specs:     make(map[string]maskSpec),
		uuidCache: make(map[string]string),
		seqCache:  make(map[string]int64),
	}
	for _, cfg := range configs {
		if cfg == "" {
			continue
		}
		spec, err := parseMaskSpec(cfg)
		if err != nil {
			return nil, err
		}
		if _, ok := algorithms[spec.algorithm]; !ok {
			return nil, fmt.Errorf("unknown mask algorithm %q for column %q", spec.algorithm, spec.column)
		}
		if spec.algorithm == "hmac" && (!spec.hasParam || spec.param == "") {
			return nil, fmt.Errorf("hmac mask for column %q requires a key: pass it as 'column:hmac:KEY'", spec.column)
		}
		m.specs[spec.column] = spec
	}
	return m, nil
}

func parseMaskSpec(s string) (maskSpec, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) < 2 {
		return maskSpec{}, fmt.Errorf("invalid mask config %q: expected 'column:algorithm[:param]'", s)
	}
	spec := maskSpec{
		column:    parts[0],
		algorithm: strings.ToLower(parts[1]),
	}
	if spec.column == "" || spec.algorithm == "" {
		return maskSpec{}, fmt.Errorf("invalid mask config %q: column and algorithm are required", s)
	}
	if len(parts) == 3 {
		spec.param = parts[2]
		spec.hasParam = true
	}
	return spec, nil
}

func (m *ColumnMasker) HasMasks() bool { return len(m.specs) > 0 }

// Returns the original-case column names being masked.
func (m *ColumnMasker) MaskedColumns() []string {
	out := make([]string, 0, len(m.specs))
	for _, s := range m.specs {
		out = append(out, s.column)
	}
	return out
}

// Returns the Arrow type that a masked column will produce
func (m *ColumnMasker) OutputType(columnName string, inputType arrow.DataType) (arrow.DataType, bool) {
	spec, ok := m.specs[columnName]
	if !ok {
		return nil, false
	}
	return algorithms[spec.algorithm].outputType(inputType), true
}

func (m *ColumnMasker) ValidateColumns(s *schema.TableSchema) error {
	if !m.HasMasks() || s == nil {
		return nil
	}
	have := make(map[string]bool, len(s.Columns))
	for _, c := range s.Columns {
		have[c.Name] = true
	}
	var missing []string
	for name := range m.specs {
		if !have[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("mask config references columns not in the source schema: %v", missing)
	}
	return nil
}

// Rewrites masked columns in schema with the algorithm's destination type
func (m *ColumnMasker) ApplyToSchema(s *schema.TableSchema) {
	if !m.HasMasks() || s == nil {
		return
	}
	for i := range s.Columns {
		col := &s.Columns[i]
		inputArrow := schema.DataTypeToArrowType(*col)
		outArrow, ok := m.OutputType(col.Name, inputArrow)
		if !ok {
			continue
		}
		col.DataType = arrowTypeToSchemaDataType(outArrow)
	}
}

func arrowTypeToSchemaDataType(t arrow.DataType) schema.DataType {
	switch t.ID() {
	case arrow.STRING, arrow.LARGE_STRING:
		return schema.TypeString
	case arrow.BOOL:
		return schema.TypeBoolean
	case arrow.INT8, arrow.INT16, arrow.UINT8:
		return schema.TypeInt16
	case arrow.INT32, arrow.UINT16:
		return schema.TypeInt32
	case arrow.INT64, arrow.UINT32, arrow.UINT64:
		return schema.TypeInt64
	case arrow.FLOAT32:
		return schema.TypeFloat32
	case arrow.FLOAT64:
		return schema.TypeFloat64
	case arrow.DATE32, arrow.DATE64:
		return schema.TypeDate
	case arrow.TIMESTAMP:
		return schema.TypeTimestamp
	case arrow.BINARY, arrow.LARGE_BINARY, arrow.FIXED_SIZE_BINARY:
		return schema.TypeBinary
	}
	return schema.TypeString
}

func (m *ColumnMasker) Transform(batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	if !m.HasMasks() {
		batch.Retain()
		return batch, nil
	}

	pool := memory.DefaultAllocator
	inputSchema := batch.Schema()
	numCols := int(batch.NumCols())
	numRows := int(batch.NumRows())

	cols := make([]arrow.Array, numCols)
	fields := make([]arrow.Field, numCols)

	cleanup := func(upTo int) {
		for j := 0; j < upTo; j++ {
			if cols[j] != nil {
				cols[j].Release()
			}
		}
	}

	for i := 0; i < numCols; i++ {
		field := inputSchema.Field(i)
		spec, isMasked := m.specs[field.Name]
		if !isMasked {
			cols[i] = batch.Column(i)
			cols[i].Retain()
			fields[i] = field
			continue
		}
		alg := algorithms[spec.algorithm]
		outType := alg.outputType(field.Type)
		out, err := m.maskColumn(pool, batch.Column(i), field.Type, outType, alg, spec, numRows)
		if err != nil {
			cleanup(i)
			return nil, err
		}
		cols[i] = out
		fields[i] = arrow.Field{
			Name:     field.Name,
			Type:     outType,
			Nullable: field.Nullable,
			Metadata: field.Metadata,
		}
	}

	newBatch := array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, batch.NumRows())
	cleanup(numCols)
	return newBatch, nil
}

func (m *ColumnMasker) OutputSchema(inputSchema *arrow.Schema) *arrow.Schema {
	if !m.HasMasks() {
		return inputSchema
	}
	fields := make([]arrow.Field, len(inputSchema.Fields()))
	for i, f := range inputSchema.Fields() {
		if spec, masked := m.specs[f.Name]; masked {
			f.Type = algorithms[spec.algorithm].outputType(f.Type)
		}
		fields[i] = f
	}
	return arrow.NewSchema(fields, nil)
}

func (m *ColumnMasker) maskColumn(pool memory.Allocator, col arrow.Array, inputType, outputType arrow.DataType, alg algorithmDef, spec maskSpec, n int) (arrow.Array, error) {
	builder := array.NewBuilder(pool, outputType)
	defer builder.Release()

	for i := 0; i < n; i++ {
		if col.IsNull(i) {
			builder.AppendNull()
			continue
		}
		v := col.ValueStr(i)
		result, isNull, err := alg.apply(m, v, inputType, spec.param, spec.hasParam)
		if err != nil {
			return nil, fmt.Errorf("mask %q on column %q: %w", spec.algorithm, spec.column, err)
		}
		if isNull || !appendValueToBuilder(builder, result) {
			builder.AppendNull()
		}
	}
	return builder.NewArray(), nil
}

// Appends a typed value to the given builder.
func appendValueToBuilder(builder array.Builder, value any) bool {
	switch b := builder.(type) {
	case *array.StringBuilder:
		switch v := value.(type) {
		case string:
			b.Append(v)
		default:
			b.Append(fmt.Sprintf("%v", v))
		}
	case *array.Int64Builder:
		switch v := value.(type) {
		case int64:
			b.Append(v)
		case float64:
			b.Append(int64(v))
		default:
			return false
		}
	case *array.Float64Builder:
		switch v := value.(type) {
		case float64:
			b.Append(v)
		case int64:
			b.Append(float64(v))
		default:
			return false
		}
	case *array.Date32Builder:
		v, ok := value.(arrow.Date32)
		if !ok {
			return false
		}
		b.Append(v)
	case *array.Date64Builder:
		v, ok := value.(arrow.Date64)
		if !ok {
			return false
		}
		b.Append(v)
	case *array.TimestampBuilder:
		v, ok := value.(arrow.Timestamp)
		if !ok {
			return false
		}
		b.Append(v)
	default:
		return false
	}
	return true
}

// ---- output-type helpers ----

func stringOutput(_ arrow.DataType) arrow.DataType { return arrow.BinaryTypes.String }
func int64Output(_ arrow.DataType) arrow.DataType  { return arrow.PrimitiveTypes.Int64 }

func preserveNumericOutput(in arrow.DataType) arrow.DataType {
	if isIntegerArrowType(in) {
		return arrow.PrimitiveTypes.Int64
	}
	if isFloatArrowType(in) {
		return arrow.PrimitiveTypes.Float64
	}
	return arrow.BinaryTypes.String
}

func preserveDateOutput(in arrow.DataType) arrow.DataType {
	switch in.ID() {
	case arrow.DATE32, arrow.DATE64, arrow.TIMESTAMP:
		return in
	}
	return arrow.BinaryTypes.String
}

func isIntegerArrowType(t arrow.DataType) bool {
	switch t.ID() {
	case arrow.INT8, arrow.INT16, arrow.INT32, arrow.INT64,
		arrow.UINT8, arrow.UINT16, arrow.UINT32, arrow.UINT64:
		return true
	}
	return false
}

func isFloatArrowType(t arrow.DataType) bool {
	switch t.ID() {
	case arrow.FLOAT32, arrow.FLOAT64:
		return true
	}
	return false
}

// ---- algorithm registry ----

var algorithms = map[string]algorithmDef{
	"hash":         {apply: algoSHA256, outputType: stringOutput},
	"sha256":       {apply: algoSHA256, outputType: stringOutput},
	"md5":          {apply: algoMD5, outputType: stringOutput},
	"hmac":         {apply: algoHMAC, outputType: stringOutput},
	"email":        {apply: algoEmail, outputType: stringOutput},
	"phone":        {apply: algoPhone, outputType: stringOutput},
	"credit_card":  {apply: algoCreditCard, outputType: stringOutput},
	"ssn":          {apply: algoSSN, outputType: stringOutput},
	"redact":       {apply: algoRedact, outputType: stringOutput},
	"stars":        {apply: algoStars, outputType: stringOutput},
	"fixed":        {apply: algoFixed, outputType: stringOutput},
	"random":       {apply: algoRandom, outputType: preserveNumericOutput},
	"partial":      {apply: algoPartial, outputType: stringOutput},
	"first_letter": {apply: algoFirstLetter, outputType: stringOutput},
	"uuid":         {apply: algoUUID, outputType: stringOutput},
	"sequential":   {apply: algoSequential, outputType: int64Output},
	"round":        {apply: algoRound, outputType: int64Output},
	"range":        {apply: algoRange, outputType: stringOutput},
	"noise":        {apply: algoNoise, outputType: preserveNumericOutput},
	"date_shift":   {apply: algoDateShift, outputType: preserveDateOutput},
	"year_only":    {apply: algoYearOnly, outputType: int64Output},
	"month_year":   {apply: algoMonthYear, outputType: stringOutput},
}

var nonDigitsRe = regexp.MustCompile(`\D`)

// ---- hash algorithms ----

func algoSHA256(_ *ColumnMasker, v string, _ arrow.DataType, _ string, _ bool) (any, bool, error) {
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:]), false, nil
}

func algoMD5(_ *ColumnMasker, v string, _ arrow.DataType, _ string, _ bool) (any, bool, error) {
	h := md5.Sum([]byte(v))
	return hex.EncodeToString(h[:]), false, nil
}

func algoHMAC(_ *ColumnMasker, v string, _ arrow.DataType, param string, hasParam bool) (any, bool, error) {
	mac := hmac.New(sha256.New, []byte(param))
	_, _ = mac.Write([]byte(v))
	return hex.EncodeToString(mac.Sum(nil)), false, nil
}

// ---- format-preserving ----

func algoEmail(_ *ColumnMasker, v string, _ arrow.DataType, _ string, _ bool) (any, bool, error) {
	if v == "" {
		return v, false, nil
	}
	at := strings.Index(v, "@")
	if at < 0 {
		return partialMaskString(v, 2), false, nil
	}
	local, domain := v[:at], v[at:]
	localRunes := []rune(local)
	n := len(localRunes)
	if n <= 2 {
		return strings.Repeat("*", n) + domain, false, nil
	}
	return string(localRunes[0]) + strings.Repeat("*", n-2) + string(localRunes[n-1]) + domain, false, nil
}

func algoPhone(_ *ColumnMasker, v string, _ arrow.DataType, _ string, _ bool) (any, bool, error) {
	if v == "" {
		return v, false, nil
	}
	digits := nonDigitsRe.ReplaceAllString(v, "")
	if len(digits) < 10 {
		return strings.Repeat("*", len(digits)), false, nil
	}
	return digits[:3] + "-***-****", false, nil
}

func algoCreditCard(_ *ColumnMasker, v string, _ arrow.DataType, _ string, _ bool) (any, bool, error) {
	if v == "" {
		return v, false, nil
	}
	digits := nonDigitsRe.ReplaceAllString(v, "")
	if len(digits) < 12 {
		return strings.Repeat("*", len(digits)), false, nil
	}
	return strings.Repeat("*", len(digits)-4) + digits[len(digits)-4:], false, nil
}

func algoSSN(_ *ColumnMasker, v string, _ arrow.DataType, _ string, _ bool) (any, bool, error) {
	if v == "" {
		return v, false, nil
	}
	digits := nonDigitsRe.ReplaceAllString(v, "")
	if len(digits) != 9 {
		return strings.Repeat("*", len(digits)), false, nil
	}
	return "***-**-" + digits[len(digits)-4:], false, nil
}

// ---- redaction ----

func algoRedact(_ *ColumnMasker, _ string, _ arrow.DataType, _ string, _ bool) (any, bool, error) {
	return "REDACTED", false, nil
}

func algoStars(_ *ColumnMasker, v string, _ arrow.DataType, _ string, _ bool) (any, bool, error) {
	return strings.Repeat("*", len([]rune(v))), false, nil
}

func algoFixed(_ *ColumnMasker, _ string, _ arrow.DataType, param string, hasParam bool) (any, bool, error) {
	if hasParam {
		return param, false, nil
	}
	return "MASKED", false, nil
}

// ---- partial ----

func algoPartial(_ *ColumnMasker, v string, _ arrow.DataType, param string, _ bool) (any, bool, error) {
	chars := 2
	if param != "" {
		if n, err := strconv.Atoi(param); err == nil && n > 0 {
			chars = n
		}
	}
	return partialMaskString(v, chars), false, nil
}

func partialMaskString(v string, chars int) string {
	if v == "" {
		return v
	}
	runes := []rune(v)
	n := len(runes)
	if n <= chars*2 {
		return strings.Repeat("*", n)
	}
	return string(runes[:chars]) + strings.Repeat("*", n-chars*2) + string(runes[n-chars:])
}

func algoFirstLetter(_ *ColumnMasker, v string, _ arrow.DataType, _ string, _ bool) (any, bool, error) {
	runes := []rune(v)
	if len(runes) <= 1 {
		return v, false, nil
	}
	return string(runes[0]) + strings.Repeat("*", len(runes)-1), false, nil
}

// ---- tokenization ----

func algoUUID(e *ColumnMasker, v string, _ arrow.DataType, _ string, _ bool) (any, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if existing, ok := e.uuidCache[v]; ok {
		return existing, false, nil
	}
	u := uuid.New().String()
	e.uuidCache[v] = u
	return u, false, nil
}

func algoSequential(e *ColumnMasker, v string, _ arrow.DataType, _ string, _ bool) (any, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if existing, ok := e.seqCache[v]; ok {
		return existing, false, nil
	}
	e.seqNext++
	e.seqCache[v] = e.seqNext
	return e.seqNext, false, nil
}

// ---- numeric ----

func algoRound(_ *ColumnMasker, v string, _ arrow.DataType, param string, _ bool) (any, bool, error) {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil, true, nil
	}
	precision := 10.0
	if param != "" {
		if p, err := strconv.ParseFloat(param, 64); err == nil && p != 0 {
			precision = p
		}
	}
	return int64(math.Round(f/precision) * precision), false, nil
}

func algoRange(_ *ColumnMasker, v string, _ arrow.DataType, param string, _ bool) (any, bool, error) {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil, true, nil
	}
	bucket := 100.0
	if param != "" {
		if p, err := strconv.ParseFloat(param, 64); err == nil && p > 0 {
			bucket = p
		}
	}
	lower := int64(math.Floor(f/bucket)) * int64(bucket)
	upper := lower + int64(bucket)
	return fmt.Sprintf("%d-%d", lower, upper), false, nil
}

func algoNoise(_ *ColumnMasker, v string, inputType arrow.DataType, param string, _ bool) (any, bool, error) {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil, true, nil
	}
	level := 0.1
	if param != "" {
		if p, err := strconv.ParseFloat(param, 64); err == nil {
			level = p
		}
	}
	magnitude := math.Abs(f)
	if magnitude == 0 {
		magnitude = 1.0
	}
	noise := (rand.Float64()*2 - 1) * level * magnitude
	result := f + noise
	if isIntegerArrowType(inputType) {
		return int64(result), false, nil
	}
	return result, false, nil
}

func algoRandom(_ *ColumnMasker, v string, inputType arrow.DataType, _ string, _ bool) (any, bool, error) {
	if isIntegerArrowType(inputType) {
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, true, nil
		}
		digits := len(strconv.FormatInt(absI(i), 10))
		low := int64(1)
		for d := 1; d < digits; d++ {
			low *= 10
		}
		high := low * 10
		return low + rand.Int64N(high-low), false, nil
	}
	if isFloatArrowType(inputType) {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, true, nil
		}
		magnitude := math.Abs(f)
		if magnitude == 0 {
			magnitude = 1.0
		}
		return rand.Float64() * 2 * magnitude, false, nil
	}
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	out := make([]byte, len([]rune(v)))
	for i := range out {
		out[i] = chars[rand.IntN(len(chars))]
	}
	return string(out), false, nil
}

// ---- dates ----

func parseDateValue(v string) (time.Time, bool) {
	t, err := dateparse.ParseAny(v)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func algoDateShift(_ *ColumnMasker, v string, inputType arrow.DataType, param string, _ bool) (any, bool, error) {
	t, ok := parseDateValue(v)
	if !ok {
		return nil, true, nil
	}
	maxDays := 30
	if param != "" {
		if p, err := strconv.Atoi(param); err == nil && p > 0 {
			maxDays = p
		}
	}
	shift := rand.IntN(2*maxDays+1) - maxDays
	shifted := t.AddDate(0, 0, shift)

	switch t := inputType.(type) {
	case *arrow.Date32Type:
		return arrow.Date32FromTime(shifted), false, nil
	case *arrow.Date64Type:
		return arrow.Date64FromTime(shifted), false, nil
	case *arrow.TimestampType:
		return timestampForUnit(shifted, t.Unit), false, nil
	}
	return shifted.Format("2006-01-02"), false, nil
}

func timestampForUnit(t time.Time, unit arrow.TimeUnit) arrow.Timestamp {
	switch unit {
	case arrow.Second:
		return arrow.Timestamp(t.Unix())
	case arrow.Millisecond:
		return arrow.Timestamp(t.UnixMilli())
	case arrow.Microsecond:
		return arrow.Timestamp(t.UnixMicro())
	case arrow.Nanosecond:
		return arrow.Timestamp(t.UnixNano())
	}
	return arrow.Timestamp(t.UnixMicro())
}

func algoYearOnly(_ *ColumnMasker, v string, _ arrow.DataType, _ string, _ bool) (any, bool, error) {
	t, ok := parseDateValue(v)
	if !ok {
		return nil, true, nil
	}
	return int64(t.Year()), false, nil
}

func algoMonthYear(_ *ColumnMasker, v string, _ arrow.DataType, _ string, _ bool) (any, bool, error) {
	t, ok := parseDateValue(v)
	if !ok {
		return nil, true, nil
	}
	return fmt.Sprintf("%d-%02d", t.Year(), t.Month()), false, nil
}

func absI(i int64) int64 {
	if i < 0 {
		return -i
	}
	return i
}
