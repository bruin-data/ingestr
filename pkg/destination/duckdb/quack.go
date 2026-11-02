package duckdb

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// inlineQuackArgs replaces ? and $N placeholders with SQL literals: a Quack
// attached catalog does not forward bound parameters to the server.
func inlineQuackArgs(sql string, args []interface{}) (string, error) {
	var b strings.Builder
	next := 0
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch {
		case c == '\'' || c == '"':
			end := i + 1
			for end < len(sql) {
				if sql[end] == c {
					if end+1 < len(sql) && sql[end+1] == c {
						end += 2
						continue
					}
					break
				}
				end++
			}
			b.WriteString(sql[i:min(end+1, len(sql))])
			i = end
		case c == '?':
			if next >= len(args) {
				return "", fmt.Errorf("quack: query has more placeholders than arguments")
			}
			b.WriteString(quackLiteral(args[next]))
			next++
		case c == '$' && i+1 < len(sql) && sql[i+1] >= '0' && sql[i+1] <= '9':
			end := i + 1
			for end < len(sql) && sql[end] >= '0' && sql[end] <= '9' {
				end++
			}
			n, _ := strconv.Atoi(sql[i+1 : end])
			if n < 1 || n > len(args) {
				return "", fmt.Errorf("quack: placeholder $%d has no argument", n)
			}
			b.WriteString(quackLiteral(args[n-1]))
			i = end - 1
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), nil
}

func quackLiteral(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case *time.Time:
		if x == nil {
			return "NULL"
		}
		return quackLiteral(*x)
	case time.Time:
		return "TIMESTAMP '" + x.UTC().Format("2006-01-02 15:04:05.999999") + "'"
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint:
		return strconv.FormatUint(uint64(x), 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case float32:
		return "'" + strconv.FormatFloat(float64(x), 'g', -1, 64) + "'::DOUBLE"
	case float64:
		return "'" + strconv.FormatFloat(x, 'g', -1, 64) + "'::DOUBLE"
	case string:
		return "'" + strings.ReplaceAll(x, "'", "''") + "'"
	case []byte:
		return "from_hex('" + hex.EncodeToString(x) + "')"
	default:
		return "'" + strings.ReplaceAll(fmt.Sprintf("%v", x), "'", "''") + "'"
	}
}
