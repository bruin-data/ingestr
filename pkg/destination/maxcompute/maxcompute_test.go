package maxcompute

import (
	"errors"
	"net/http"
	"testing"

	"github.com/aliyun/aliyun-odps-go-sdk/odps/restclient"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestExtractValueDefaultUsesRowValue(t *testing.T) {
	t.Parallel()

	builder := array.NewUint8Builder(memory.DefaultAllocator)
	defer builder.Release()
	builder.AppendValues([]uint8{1, 2}, nil)
	arr := builder.NewArray()
	defer arr.Release()

	if got := extractValue(arr, 1); got != "2" {
		t.Fatalf("extractValue default = %#v, want row value", got)
	}
}

func TestExtractValueTime64Nanoseconds(t *testing.T) {
	t.Parallel()

	builder := array.NewTime64Builder(memory.DefaultAllocator, arrow.FixedWidthTypes.Time64ns.(*arrow.Time64Type))
	defer builder.Release()
	builder.Append(arrow.Time64((1*3600+2*60+3)*1_000_000_000 + 4_000))
	arr := builder.NewArray()
	defer arr.Release()

	if got := extractValue(arr, 0); got != "01:02:03.000004" {
		t.Fatalf("extractValue Time64ns = %#v, want 01:02:03.000004", got)
	}
}

func TestIsNotFoundErrorRequiresTypedHTTP404(t *testing.T) {
	t.Parallel()

	if !isNotFoundError(restclient.HttpError{StatusCode: http.StatusNotFound}) {
		t.Fatal("isNotFoundError should match typed 404")
	}
	if isNotFoundError(errors.New("DNS: host not found")) {
		t.Fatal("isNotFoundError should not match arbitrary not found text")
	}
}
