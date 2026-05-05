package redshift

import "testing"

func TestNormalizeURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain redshift",
			in:   "redshift://user:password@host:5439/dbname?sslmode=require",
			want: "postgres://user:password@host:5439/dbname?sslmode=require",
		},
		{
			name: "chorus host missing at",
			in:   "redshift://user:password<CHORUS_TAG>host</CHORUS_TAG>:5439/dbname?sslmode=require",
			want: "postgres://user:password@host:5439/dbname?sslmode=require",
		},
		{
			name: "chorus host no credentials",
			in:   "redshift://<CHORUS_TAG>host</CHORUS_TAG>:5439/dbname?sslmode=require",
			want: "postgres://host:5439/dbname?sslmode=require",
		},
		{
			name: "redshift plus scheme",
			in:   "redshift+psycopg2://user:password@host:5439/dbname?sslmode=require",
			want: "postgres://user:password@host:5439/dbname?sslmode=require",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NormalizeURI(tt.in)
			if got != tt.want {
				t.Fatalf("NormalizeURI() = %q, want %q", got, tt.want)
			}
		})
	}
}
