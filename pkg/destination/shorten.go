package destination

import (
	"encoding/base64"
	"strings"
	"unicode/utf8"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/tablename"
	"golang.org/x/crypto/sha3"
)

var maxIdentifierLengths = map[string]int{
	"postgres":            63, // NAMEDATALEN = 64 - 1 null byte
	"postgresql":          63,
	"postgresql+psycopg2": 63,
	"mysql":               64,
	"mysql+pymysql":       64,
	"mariadb":             64,
	"oracle":              128,
	"oracle+cx_oracle":    128,
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

// ResolveMultiTableName builds the destination name for one source table and
// shortens only its final table component to the destination's identifier
// limit. Qualification and identifier delimiters from custom namers are kept
// intact, while the full unshortened physical path seeds the collision tag.
func ResolveMultiTableName(scheme string, namer MultiTableNamer, destSchema, sourceTable string) string {
	name := DefaultMultiTableName(destSchema, sourceTable)
	if namer != nil {
		name = namer.DestTableName(destSchema, sourceTable)
	}
	maxLen := MaxIdentifierLength(scheme)
	if maxLen <= 0 {
		return name
	}

	rawParts := tablename.SplitRaw(name)
	parts := tablename.Split(name)
	if len(rawParts) == 0 || len(rawParts) != len(parts) {
		return name
	}
	tableIndex := len(parts) - 1
	hashSource := CDCTargetKey(parts...)
	shortened := ShortenIdentifier(parts[tableIndex], hashSource, maxLen)
	if shortened == parts[tableIndex] {
		return name
	}
	rawParts[tableIndex] = renderIdentifierLike(rawParts[tableIndex], shortened)
	return strings.Join(rawParts, ".")
}

func renderIdentifierLike(raw, identifier string) string {
	if len(raw) < 2 {
		return identifier
	}
	switch {
	case raw[0] == '"' && raw[len(raw)-1] == '"':
		return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
	case raw[0] == '`' && raw[len(raw)-1] == '`':
		return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
	case raw[0] == '[' && raw[len(raw)-1] == ']':
		return "[" + strings.ReplaceAll(identifier, "]", "]]") + "]"
	default:
		return identifier
	}
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
	prefixBudget := remaining/2 + overflow
	suffixBudget := remaining / 2

	prefix := utf8PrefixBoundary(name, prefixBudget)
	suffixStart := utf8SuffixBoundary(name, suffixBudget)
	spare := remaining - prefix - (len(name) - suffixStart)

	// A target byte offset can bisect a multi-byte rune. Reuse any bytes left
	// by boundary adjustment when the adjacent whole rune still fits.
	for spare > 0 && prefix < suffixStart {
		_, size := utf8.DecodeRuneInString(name[prefix:])
		if size <= 0 || size > spare || prefix+size > suffixStart {
			break
		}
		prefix += size
		spare -= size
	}
	for spare > 0 && prefix < suffixStart {
		_, size := utf8.DecodeLastRuneInString(name[:suffixStart])
		if size <= 0 || size > spare || suffixStart-size < prefix {
			break
		}
		suffixStart -= size
		spare -= size
	}

	return name[:prefix] + tag + name[suffixStart:]
}

func utf8PrefixBoundary(name string, budget int) int {
	end := min(budget, len(name))
	for end > 0 && end < len(name) && !utf8.RuneStart(name[end]) {
		end--
	}
	return end
}

func utf8SuffixBoundary(name string, budget int) int {
	start := max(0, len(name)-budget)
	for start < len(name) && !utf8.RuneStart(name[start]) {
		start++
	}
	return start
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
		if current, ok := reverseMap[norm]; !ok || orig < current {
			reverseMap[norm] = orig
		}
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
