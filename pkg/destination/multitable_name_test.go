package destination

import "testing"

func TestDefaultMultiTableName(t *testing.T) {
	tests := []struct {
		name        string
		destSchema  string
		sourceTable string
		want        string
	}{
		{"mirror unqualified", "", "orders", "orders"},
		{"mirror schema-qualified", "", "dbo.orders", "dbo.orders"},
		{"funnel unqualified", "raw", "orders", "raw.orders"},
		{"funnel flattens source schema", "raw", "dbo.orders", "raw.dbo_orders"},
		{"funnel flattens multiple dots", "raw", "srv.db.dbo.orders", "raw.srv_db_dbo_orders"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DefaultMultiTableName(tt.destSchema, tt.sourceTable); got != tt.want {
				t.Errorf("DefaultMultiTableName(%q, %q) = %q, want %q", tt.destSchema, tt.sourceTable, got, tt.want)
			}
		})
	}
}
