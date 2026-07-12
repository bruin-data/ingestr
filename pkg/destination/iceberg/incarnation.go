package iceberg

import (
	"context"
	"fmt"
	"strings"

	icebergtable "github.com/apache/iceberg-go/table"
)

func (d *Destination) validateExpectedIncarnation(
	ctx context.Context,
	tbl *icebergtable.Table,
	expected string,
) error {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return nil
	}
	if actual := tbl.Metadata().TableUUID().String(); actual != expected {
		return fmt.Errorf(
			"iceberg: target incarnation changed for %s: expected UUID %s, got %s",
			strings.Join(tbl.Identifier(), "."), expected, actual,
		)
	}
	current, err := d.catalog.LoadTable(ctx, tbl.Identifier())
	if err != nil {
		return fmt.Errorf("iceberg: failed to validate target incarnation for %s: %w", strings.Join(tbl.Identifier(), "."), err)
	}
	if actual := current.Metadata().TableUUID().String(); actual != expected {
		return fmt.Errorf(
			"iceberg: target incarnation changed for %s: expected UUID %s, got %s",
			strings.Join(tbl.Identifier(), "."), expected, actual,
		)
	}
	return nil
}
