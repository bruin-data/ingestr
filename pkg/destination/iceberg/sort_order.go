package iceberg

import (
	"context"
	"fmt"

	iceberggo "github.com/apache/iceberg-go"
	icebergtable "github.com/apache/iceberg-go/table"
)

func sortOrderForColumns(sc *iceberggo.Schema, columns []string, orderID int) (icebergtable.SortOrder, error) {
	fields := make([]icebergtable.SortField, len(columns))
	for i, column := range columns {
		field, ok := sc.FindFieldByName(column)
		if !ok {
			return icebergtable.SortOrder{}, fmt.Errorf("iceberg: cluster column %q not found", column)
		}
		fields[i] = icebergtable.SortField{
			SourceIDs: []int{field.ID},
			Transform: iceberggo.IdentityTransform{},
			Direction: icebergtable.SortASC,
			NullOrder: icebergtable.NullsFirst,
		}
	}
	order, err := icebergtable.NewSortOrder(orderID, fields)
	if err != nil {
		return icebergtable.SortOrder{}, fmt.Errorf("iceberg: invalid cluster columns: %w", err)
	}
	if err := order.CheckCompatibility(sc); err != nil {
		return icebergtable.SortOrder{}, fmt.Errorf("iceberg: invalid cluster columns: %w", err)
	}
	return order, nil
}

func (d *Destination) ensureSortOrder(ctx context.Context, tbl *icebergtable.Table, columns []string) (*icebergtable.Table, error) {
	return d.ensureSortOrderExpected(ctx, tbl, columns, "")
}

func (d *Destination) ensureSortOrderExpected(
	ctx context.Context,
	tbl *icebergtable.Table,
	columns []string,
	expectedIncarnation string,
) (*icebergtable.Table, error) {
	if err := d.validateExpectedIncarnation(ctx, tbl, expectedIncarnation); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		if tbl.SortOrder().OrderID() == icebergtable.UnsortedSortOrder.OrderID() {
			return tbl, nil
		}
		// A table created with a sort order carries only that order in its
		// metadata; the unsorted order must be added before it can become the
		// default.
		updates := make([]icebergtable.Update, 0, 2)
		hasUnsorted := false
		for _, existing := range tbl.Metadata().SortOrders() {
			if existing.OrderID() == icebergtable.UnsortedSortOrder.OrderID() {
				hasUnsorted = true
				break
			}
		}
		if !hasUnsorted {
			updates = append(updates, icebergtable.NewAddSortOrderUpdate(&icebergtable.UnsortedSortOrder))
		}
		updates = append(updates, icebergtable.NewSetDefaultSortOrderUpdate(icebergtable.UnsortedSortOrder.OrderID()))
		if err := d.validateExpectedIncarnation(ctx, tbl, expectedIncarnation); err != nil {
			return nil, err
		}
		_, _, err := d.catalog.CommitTable(ctx, tbl.Identifier(), []icebergtable.Requirement{
			icebergtable.AssertTableUUID(tbl.Metadata().TableUUID()),
			icebergtable.AssertDefaultSortOrderID(tbl.SortOrder().OrderID()),
		}, updates)
		if err != nil {
			return nil, fmt.Errorf("iceberg: failed to clear sort order: %w", err)
		}
		refreshed, err := d.catalog.LoadTable(ctx, tbl.Identifier())
		if err != nil {
			return nil, fmt.Errorf("iceberg: failed to reload table after clearing sort order: %w", err)
		}
		if err := d.validateExpectedIncarnation(ctx, refreshed, expectedIncarnation); err != nil {
			return nil, err
		}
		return refreshed, nil
	}
	desired, err := sortOrderForColumns(tbl.Schema(), columns, 1)
	if err != nil {
		return nil, err
	}

	matchingID := -1
	maxID := 0
	for _, existing := range tbl.Metadata().SortOrders() {
		if existing.OrderID() > maxID {
			maxID = existing.OrderID()
		}
		if sortFieldsEqual(existing, desired) {
			matchingID = existing.OrderID()
		}
	}
	if matchingID == tbl.SortOrder().OrderID() {
		return tbl, nil
	}

	updates := make([]icebergtable.Update, 0, 2)
	if matchingID < 0 {
		desired, err = sortOrderForColumns(tbl.Schema(), columns, maxID+1)
		if err != nil {
			return nil, err
		}
		matchingID = desired.OrderID()
		updates = append(updates, icebergtable.NewAddSortOrderUpdate(&desired))
	}
	updates = append(updates, icebergtable.NewSetDefaultSortOrderUpdate(matchingID))
	if err := d.validateExpectedIncarnation(ctx, tbl, expectedIncarnation); err != nil {
		return nil, err
	}
	_, _, err = d.catalog.CommitTable(ctx, tbl.Identifier(), []icebergtable.Requirement{
		icebergtable.AssertTableUUID(tbl.Metadata().TableUUID()),
		icebergtable.AssertDefaultSortOrderID(tbl.SortOrder().OrderID()),
	}, updates)
	if err != nil {
		return nil, fmt.Errorf("iceberg: failed to update sort order: %w", err)
	}
	refreshed, err := d.catalog.LoadTable(ctx, tbl.Identifier())
	if err != nil {
		return nil, fmt.Errorf("iceberg: failed to reload table after sort order update: %w", err)
	}
	if err := d.validateExpectedIncarnation(ctx, refreshed, expectedIncarnation); err != nil {
		return nil, err
	}
	return refreshed, nil
}

func identitySortColumns(tbl *icebergtable.Table) ([]string, bool) {
	columns := make([]string, 0, tbl.SortOrder().Len())
	for _, field := range tbl.SortOrder().Fields() {
		if len(field.SourceIDs) != 1 {
			return nil, false
		}
		if _, ok := field.Transform.(iceberggo.IdentityTransform); !ok || field.Direction != icebergtable.SortASC || field.NullOrder != icebergtable.NullsFirst {
			return nil, false
		}
		name, ok := tbl.Schema().FindColumnName(field.SourceIDs[0])
		if !ok {
			return nil, false
		}
		columns = append(columns, name)
	}
	return columns, true
}

func sortFieldsEqual(left, right icebergtable.SortOrder) bool {
	if left.Len() != right.Len() {
		return false
	}
	rightFields := make([]icebergtable.SortField, 0, right.Len())
	for _, field := range right.Fields() {
		rightFields = append(rightFields, field)
	}
	i := 0
	for _, field := range left.Fields() {
		if !field.Equals(rightFields[i]) {
			return false
		}
		i++
	}
	return true
}

func sortOrderMatchesColumns(tbl *icebergtable.Table, columns []string) bool {
	if len(columns) == 0 {
		return tbl.SortOrder().IsUnsorted()
	}
	desired, err := sortOrderForColumns(tbl.Schema(), columns, icebergtable.InitialSortOrderID)
	return err == nil && sortFieldsEqual(tbl.SortOrder(), desired)
}

func withDataFileSortOrderID(dataFile iceberggo.DataFile, tbl *icebergtable.Table, sortOrderID int) (iceberggo.DataFile, error) {
	if current := dataFile.SortOrderID(); current != nil && *current == sortOrderID {
		return dataFile, nil
	}
	return copyDataFileMetadata(dataFile, tbl, &sortOrderID, nil)
}

func withDataFileFirstRowID(dataFile iceberggo.DataFile, tbl *icebergtable.Table, firstRowID int64) (iceberggo.DataFile, error) {
	if current := dataFile.FirstRowID(); current != nil && *current == firstRowID {
		return dataFile, nil
	}
	return copyDataFileMetadata(dataFile, tbl, nil, &firstRowID)
}

func copyDataFileMetadata(
	dataFile iceberggo.DataFile,
	tbl *icebergtable.Table,
	sortOrderID *int,
	firstRowID *int64,
) (iceberggo.DataFile, error) {
	logicalTypes := make(map[int]string)
	fixedSizes := make(map[int]int)
	spec := tbl.Spec()
	for _, partitionField := range spec.Fields() {
		sourceField, ok := tbl.Schema().FindFieldByID(partitionField.SourceID())
		if !ok {
			return nil, fmt.Errorf("iceberg: partition source field %d not found", partitionField.SourceID())
		}
		switch resultType := partitionField.Transform.ResultType(sourceField.Type).(type) {
		case iceberggo.DateType:
			logicalTypes[partitionField.FieldID] = "date"
		case iceberggo.TimeType:
			logicalTypes[partitionField.FieldID] = "time-micros"
		case iceberggo.TimestampType, iceberggo.TimestampTzType:
			logicalTypes[partitionField.FieldID] = "timestamp-micros"
		case iceberggo.DecimalType:
			logicalTypes[partitionField.FieldID] = "decimal"
			fixedSizes[partitionField.FieldID] = resultType.Scale()
		case iceberggo.UUIDType:
			logicalTypes[partitionField.FieldID] = "uuid"
		}
	}

	builder, err := iceberggo.NewDataFileBuilder(
		spec,
		dataFile.ContentType(),
		dataFile.FilePath(),
		dataFile.FileFormat(),
		dataFile.Partition(),
		logicalTypes,
		fixedSizes,
		dataFile.Count(),
		dataFile.FileSizeBytes(),
	)
	if err != nil {
		return nil, fmt.Errorf("iceberg: failed to copy data file metadata: %w", err)
	}
	builder.ColumnSizes(dataFile.ColumnSizes()).
		ValueCounts(dataFile.ValueCounts()).
		NullValueCounts(dataFile.NullValueCounts()).
		NaNValueCounts(dataFile.NaNValueCounts()).
		LowerBoundValues(dataFile.LowerBoundValues()).
		UpperBoundValues(dataFile.UpperBoundValues())
	if sortOrderID != nil {
		builder.SortOrderID(*sortOrderID)
	} else if current := dataFile.SortOrderID(); current != nil {
		builder.SortOrderID(*current)
	}
	if key := dataFile.KeyMetadata(); key != nil {
		builder.KeyMetadata(key)
	}
	if offsets := dataFile.SplitOffsets(); offsets != nil {
		builder.SplitOffsets(offsets)
	}
	if fieldIDs := dataFile.EqualityFieldIDs(); fieldIDs != nil {
		builder.EqualityFieldIDs(fieldIDs)
	}
	if firstRowID != nil {
		builder.FirstRowID(*firstRowID)
	} else if current := dataFile.FirstRowID(); current != nil {
		builder.FirstRowID(*current)
	}
	if referencedDataFile := dataFile.ReferencedDataFile(); referencedDataFile != nil {
		builder.ReferencedDataFile(*referencedDataFile)
	}
	if offset := dataFile.ContentOffset(); offset != nil {
		builder.ContentOffset(*offset)
	}
	if size := dataFile.ContentSizeInBytes(); size != nil {
		builder.ContentSizeInBytes(*size)
	}
	return builder.Build(), nil
}
