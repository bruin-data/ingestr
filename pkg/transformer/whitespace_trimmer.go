package transformer

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

type WhitespaceTrimmer struct{}

func NewWhitespaceTrimmer() *WhitespaceTrimmer {
	return &WhitespaceTrimmer{}
}

func (t *WhitespaceTrimmer) Transform(batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	inputSchema := batch.Schema()
	if !hasStringFields(inputSchema) {
		batch.Retain()
		return batch, nil
	}

	numCols := int(batch.NumCols())
	cols := make([]arrow.Array, numCols)

	cleanup := func(upTo int) {
		for i := 0; i < upTo; i++ {
			if cols[i] != nil {
				cols[i].Release()
			}
		}
	}

	for i := 0; i < numCols; i++ {
		field := inputSchema.Field(i)
		col := batch.Column(i)
		if !isStringLikeType(field.Type) {
			cols[i] = col
			cols[i].Retain()
			continue
		}

		trimmed, err := trimColumn(col, field.Type)
		if err != nil {
			cleanup(i)
			return nil, fmt.Errorf("trim whitespace for column %q: %w", field.Name, err)
		}
		cols[i] = trimmed
	}

	newBatch := array.NewRecordBatch(inputSchema, cols, batch.NumRows())
	cleanup(numCols)
	return newBatch, nil
}

func (t *WhitespaceTrimmer) OutputSchema(inputSchema *arrow.Schema) *arrow.Schema {
	return inputSchema
}

func hasStringFields(s *arrow.Schema) bool {
	for _, field := range s.Fields() {
		if isStringLikeType(field.Type) {
			return true
		}
	}
	return false
}

func isStringLikeType(dt arrow.DataType) bool {
	switch dt.ID() {
	case arrow.STRING, arrow.LARGE_STRING:
		return true
	case arrow.DICTIONARY:
		valueType := dt.(*arrow.DictionaryType).ValueType
		return valueType.ID() == arrow.STRING || valueType.ID() == arrow.LARGE_STRING
	default:
		return false
	}
}

func trimColumn(col arrow.Array, dt arrow.DataType) (arrow.Array, error) {
	if dictType, ok := dt.(*arrow.DictionaryType); ok {
		return trimDictionaryColumn(col, dictType)
	}
	return trimStringColumn(col, dt)
}

func trimStringColumn(col arrow.Array, dt arrow.DataType) (arrow.Array, error) {
	builder := array.NewBuilder(memory.DefaultAllocator, dt)
	defer builder.Release()
	builder.Reserve(col.Len())

	for i := 0; i < col.Len(); i++ {
		if col.IsNull(i) {
			builder.AppendNull()
			continue
		}
		if err := builder.AppendValueFromString(strings.TrimSpace(col.ValueStr(i))); err != nil {
			return nil, fmt.Errorf("row %d: %w", i, err)
		}
	}

	return builder.NewArray(), nil
}

func trimDictionaryColumn(col arrow.Array, dt *arrow.DictionaryType) (arrow.Array, error) {
	dict, ok := col.(*array.Dictionary)
	if !ok {
		return nil, fmt.Errorf("expected dictionary array, got %T", col)
	}

	valueBuilder := array.NewBuilder(memory.DefaultAllocator, dt.ValueType)
	defer valueBuilder.Release()
	indexBuilder := array.NewBuilder(memory.DefaultAllocator, dt.IndexType)
	defer indexBuilder.Release()
	indexBuilder.Reserve(col.Len())

	valueIndex := make(map[string]int)
	dictionaryValues := dict.Dictionary()
	for i := 0; i < col.Len(); i++ {
		if col.IsNull(i) {
			indexBuilder.AppendNull()
			continue
		}

		dictionaryIndex := dict.GetValueIndex(i)
		if dictionaryValues.IsNull(dictionaryIndex) {
			indexBuilder.AppendNull()
			continue
		}

		value := strings.TrimSpace(dictionaryValues.ValueStr(dictionaryIndex))
		idx, ok := valueIndex[value]
		if !ok {
			idx = len(valueIndex)
			if err := valueBuilder.AppendValueFromString(value); err != nil {
				return nil, fmt.Errorf("dictionary value %q: %w", value, err)
			}
			valueIndex[value] = idx
		}
		if err := indexBuilder.AppendValueFromString(strconv.Itoa(idx)); err != nil {
			return nil, fmt.Errorf("dictionary index %d: %w", idx, err)
		}
	}

	indices := indexBuilder.NewArray()
	defer indices.Release()
	values := valueBuilder.NewArray()
	defer values.Release()

	return array.NewDictionaryArray(dt, indices, values), nil
}
