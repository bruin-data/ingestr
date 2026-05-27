package destination

import (
	"encoding/base64"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
	"golang.org/x/crypto/sha3"
)

var maxIdentifierLengths = map[string]int{
	"postgres":            63, // NAMEDATALEN = 64 - 1 null byte
	"postgresql":          63,
	"postgresql+psycopg2": 63,
	"mysql":               64,
	"mysql+pymysql":       64,
	"mariadb":             64,
	"mssql":               128,
	"sqlserver":           128,
	"mssql+pyodbc":        128,
	"snowflake":           255,
	"bigquery":            300,
	"trino":               128,
	"cratedb":             255,
	"synapse":             128,
	"fabric":              128,
	"clickhouse":          255, // no engine-side limit; bounded by filesystem filename length (ext4/xfs 255 bytes)
	"databricks":          255, // Unity Catalog / Spark identifier
	"duckdb":              1024,
}

func MaxIdentifierLength(scheme string) int {
	return maxIdentifierLengths[scheme]
}

// tagBytes is the SHAKE-128 digest length matching ingestr's default collision_prob=0.001:
// int(((2+1) * math.log2(1/0.001) // 8) + 1) = 4
const tagBytes = 4

// computeTag produces a collision-resistant tag identical to ingestr's _compute_tag.
// SHAKE-128 → base64 → strip padding → translate /→a +→b → lowercase.
func computeTag(identifier string) string {
	h := sha3.NewShake128()
	h.Write([]byte(identifier))
	digest := make([]byte, tagBytes)
	_, _ = h.Read(digest)
	tag := base64.StdEncoding.EncodeToString(digest)
	tag = strings.TrimRight(tag, "=")
	tag = strings.NewReplacer("/", "a", "+", "b").Replace(tag)
	return strings.ToLower(tag)
}

// ShortenIdentifier shortens a database identifier to fit within maxLen bytes.
// If the name already fits or maxLen <= 0, it is returned unchanged.
// The algorithm matches ingestr's _trim_and_tag: tag is inserted in the middle with no separators.
// hashSource is the name used for computing the tag (typically the original pre-normalization name).
//
//	name[:prefix] + tag + name[len-suffix:]
func ShortenIdentifier(name string, hashSource string, maxLen int) string {
	if maxLen <= 0 || len(name) <= maxLen {
		return name
	}

	tag := computeTag(hashSource)
	if len(tag) >= maxLen {
		return tag[:maxLen]
	}

	remaining := maxLen - len(tag)
	overflow := remaining % 2
	prefix := remaining/2 + overflow
	suffix := remaining / 2

	return name[:prefix] + tag + name[len(name)-suffix:]
}

// ShortenColumnNames builds a mapping of original → shortened column names for
// columns that exceed maxLen. Returns nil if no shortening is needed.
// renameMapping is the forward naming convention mapping (original → normalized).
// If provided, the original names are used for hash computation to match ingestr output.
func ShortenColumnNames(columns []schema.Column, maxLen int, renameMapping map[string]string) map[string]string {
	if maxLen <= 0 {
		return nil
	}

	// Build reverse lookup: normalized → original
	reverseMap := make(map[string]string, len(renameMapping))
	for orig, norm := range renameMapping {
		reverseMap[norm] = orig
	}

	mapping := make(map[string]string)
	for _, col := range columns {
		hashSource := col.Name
		if orig, ok := reverseMap[col.Name]; ok {
			hashSource = orig
		}
		shortened := ShortenIdentifier(col.Name, hashSource, maxLen)
		if shortened != col.Name {
			mapping[col.Name] = shortened
		}
	}

	if len(mapping) == 0 {
		return nil
	}

	return mapping
}
