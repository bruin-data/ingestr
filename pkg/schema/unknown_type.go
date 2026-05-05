package schema

import (
	"reflect"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

const UnknownExtensionName = "unknown"

// UnknownType is an Arrow extension type for values with unknown logical type.
// It stores values as JSON-encoded strings for later inference.
type UnknownType struct {
	arrow.ExtensionBase
}

func NewUnknownType() *UnknownType {
	return &UnknownType{
		ExtensionBase: arrow.ExtensionBase{Storage: arrow.BinaryTypes.String},
	}
}

func (t *UnknownType) ExtensionName() string {
	return UnknownExtensionName
}

func (t *UnknownType) ExtensionEquals(other arrow.ExtensionType) bool {
	return t.ExtensionName() == other.ExtensionName()
}

func (t *UnknownType) Serialize() string {
	return ""
}

func (t *UnknownType) Deserialize(storageType arrow.DataType, data string) (arrow.ExtensionType, error) {
	return NewUnknownType(), nil
}

func (t *UnknownType) ArrayType() reflect.Type {
	return reflect.TypeOf(UnknownArray{})
}

func (t *UnknownType) String() string {
	return "extension<" + UnknownExtensionName + ">"
}

// UnknownArray wraps an ExtensionArrayBase for unknown data.
type UnknownArray struct {
	array.ExtensionArrayBase
}

// UnknownBuilder builds unknown extension arrays.
type UnknownBuilder struct {
	*array.ExtensionBuilder
}

func NewUnknownBuilder(mem memory.Allocator) *UnknownBuilder {
	return &UnknownBuilder{
		ExtensionBuilder: array.NewExtensionBuilder(mem, NewUnknownType()),
	}
}

// UnknownArrowType is the singleton instance of UnknownType for use in Arrow schemas.
var UnknownArrowType = NewUnknownType()

func init() {
	_ = arrow.RegisterExtensionType(UnknownArrowType)
}
