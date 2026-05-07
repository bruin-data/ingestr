package redshift

import "strings"

const (
	chorusTagOpen  = "<CHORUS_TAG>"
	chorusTagClose = "</CHORUS_TAG>"
)

// NormalizeURI converts a Redshift-style URI into a PostgreSQL-compatible URI.
// It also strips any <CHORUS_TAG>...</CHORUS_TAG> wrappers that may appear in the URI.
func NormalizeURI(uri string) string {
	scheme, rest, ok := strings.Cut(uri, "://")
	if !ok {
		return uri
	}

	lowerScheme := strings.ToLower(scheme)
	if strings.HasPrefix(lowerScheme, "redshift") {
		uri = "postgres://" + rest
	}

	// Some systems wrap the host in CHORUS_TAG markers and omit the '@' separator:
	//   redshift://user:pass<CHORUS_TAG>host</CHORUS_TAG>:5439/db
	// Insert the missing '@' before the tag so URL parsing works after stripping tags.
	if idx := strings.Index(uri, chorusTagOpen); idx != -1 {
		if !strings.Contains(uri[:idx], "@") {
			afterScheme := strings.TrimPrefix(uri, "postgres://")
			authority := afterScheme
			if slash := strings.IndexByte(afterScheme, '/'); slash != -1 {
				authority = afterScheme[:slash]
			}
			if strings.Contains(authority, ":") { // likely user:pass present
				uri = uri[:idx] + "@" + uri[idx:]
			}
		}
	}

	uri = strings.ReplaceAll(uri, chorusTagOpen, "")
	uri = strings.ReplaceAll(uri, chorusTagClose, "")
	return uri
}
