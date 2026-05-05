package schema

import (
	"reflect"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

const JSONExtensionName = "json"

// JSONType is an Arrow extension type for JSON data.
// It stores JSON as strings but is recognized as structured data.
type JSONType struct {
	arrow.ExtensionBase
}

func NewJSONType() *JSONType {
	return &JSONType{
		ExtensionBase: arrow.ExtensionBase{Storage: arrow.BinaryTypes.String},
	}
}

func (t *JSONType) ExtensionName() string {
	return JSONExtensionName
}

func (t *JSONType) ExtensionEquals(other arrow.ExtensionType) bool {
	return t.ExtensionName() == other.ExtensionName()
}

func (t *JSONType) Serialize() string {
	return ""
}

func (t *JSONType) Deserialize(storageType arrow.DataType, data string) (arrow.ExtensionType, error) {
	return NewJSONType(), nil
}

func (t *JSONType) ArrayType() reflect.Type {
	return reflect.TypeOf(JSONArray{})
}

func (t *JSONType) String() string {
	return "extension<" + JSONExtensionName + ">"
}

// JSONArray wraps an ExtensionArrayBase for JSON data.
type JSONArray struct {
	array.ExtensionArrayBase
}

// JSONBuilder builds JSON extension arrays.
type JSONBuilder struct {
	*array.ExtensionBuilder
}

func NewJSONBuilder(mem memory.Allocator) *JSONBuilder {
	return &JSONBuilder{
		ExtensionBuilder: array.NewExtensionBuilder(mem, NewJSONType()),
	}
}

func (b *JSONBuilder) Append(v string) {
	b.ExtensionBuilder.Builder.(*array.StringBuilder).Append(v)
}

func (b *JSONBuilder) AppendNull() {
	b.ExtensionBuilder.Builder.(*array.StringBuilder).AppendNull()
}

// JSONArrowType is the singleton instance of JSONType for use in Arrow schemas.
var JSONArrowType = NewJSONType()

func init() {
	_ = arrow.RegisterExtensionType(JSONArrowType)
}
