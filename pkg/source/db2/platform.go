package db2

import (
	"fmt"
	"strings"
)

// db2Platform identifies which Db2 product family is actually serving the
// RDB on the other end of the DRDA connection. The three Db2 platforms
// (LUW, z/OS, and i/iSeries) all speak the same DRDA wire protocol, but
// expose different system catalog schemas, so query building needs to
// know which one it's talking to.
type db2Platform int

const (
	db2PlatformAuto db2Platform = iota
	db2PlatformLUW
	db2PlatformZOS
	db2PlatformIBMi
)

func (p db2Platform) String() string {
	switch p {
	case db2PlatformIBMi:
		return "ibmi"
	case db2PlatformZOS:
		return "zos"
	case db2PlatformLUW:
		return "luw"
	default:
		return "auto"
	}
}

func parseDb2PlatformOverride(value string) (db2Platform, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return db2PlatformAuto, nil
	case "luw", "db2", "linux", "unix", "windows":
		return db2PlatformLUW, nil
	case "zos", "z/os", "mainframe":
		return db2PlatformZOS, nil
	case "ibmi", "iseries", "i-series", "as400", "as/400", "i5os", "os400":
		return db2PlatformIBMi, nil
	default:
		return db2PlatformAuto, fmt.Errorf("unsupported platform %q: expected luw, zos, or ibmi", value)
	}
}

// detectDb2PlatformFromPRDID maps the DRDA PRDID value returned in the
// ACCRDBRM reply (e.g. "QSQ07030" for IBM i, "DSN12015" for z/OS,
// "SQL11050" for LUW) to the platform serving the RDB. Unrecognized
// values are left as db2PlatformAuto, which keeps the existing LUW-shaped
// catalog queries as the default.
func detectDb2PlatformFromPRDID(prdid string) db2Platform {
	prdid = strings.ToUpper(strings.TrimSpace(prdid))
	switch {
	case strings.HasPrefix(prdid, "QSQ"):
		return db2PlatformIBMi
	case strings.HasPrefix(prdid, "DSN"):
		return db2PlatformZOS
	case strings.HasPrefix(prdid, "SQL"), strings.HasPrefix(prdid, "JCC"):
		return db2PlatformLUW
	default:
		return db2PlatformAuto
	}
}
