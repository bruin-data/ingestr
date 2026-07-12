package destination

import "testing"

func TestCompareCDCPositionsOrdersPostgresLSNNumerically(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
		want  int
	}{
		{name: "low word crosses hexadecimal width", left: "0/100", right: "0/FF", want: 1},
		{name: "low word reverse ordering", left: "0/FF", right: "0/100", want: -1},
		{name: "high word carries past the maximum low word", left: "1/0", right: "0/FFFFFFFF", want: 1},
		{name: "padding does not change the position", left: "00000000/00000100", right: "0/100", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareCDCPositions(tt.left, tt.right)
			switch {
			case tt.want < 0 && got >= 0:
				t.Fatalf("CompareCDCPositions(%q, %q) = %d, want a negative result", tt.left, tt.right, got)
			case tt.want == 0 && got != 0:
				t.Fatalf("CompareCDCPositions(%q, %q) = %d, want zero", tt.left, tt.right, got)
			case tt.want > 0 && got <= 0:
				t.Fatalf("CompareCDCPositions(%q, %q) = %d, want a positive result", tt.left, tt.right, got)
			}
		})
	}
}
