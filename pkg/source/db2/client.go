package db2

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
)

type db2Client struct {
	conn     net.Conn
	host     string
	ssl      bool
	database string
	user     string
	password string
	timeout  time.Duration
	private  *big.Int
	mu       sync.Mutex
	platform db2Platform
	// rdbName is the real RDB directory name (e.g. "RDBNAME2"), distinct
	// from database which is commonly a symbolic alias like *LOCAL on
	// IBM i. ACCSEC/ACCRDB resolve the alias themselves, but PKGNAMCSN
	// needs the real name. Set directly from the URI's rdb_name override
	// if given, otherwise auto-detected from ACCRDBRM during handshake.
	rdbName string
	// signonExchange performs the Sign-on Server attribute exchange needed
	// to compute a SECMEC 8 credential (see signon.go). Left nil in normal
	// operation, where handshake falls back to the real
	// exchangeSignonAttributes; tests override it to avoid a real Sign-on
	// Server connection when only the DRDA/DDM side of the handshake is
	// under test.
	signonExchange func(context.Context, string, bool, time.Duration) (signonAttributes, error)
	// sawACCRDBRM records whether the handshake's parseResponse call
	// actually observed an ACCRDBRM reply. parseResponse returns
	// successfully once the chain ends with no error reply, even if
	// ACCRDBRM specifically never arrived - this flag lets handshake
	// tell the two cases apart instead of silently assuming success.
	sawACCRDBRM bool
}

// Platform returns the Db2 platform serving the RDB: either the value
// forced via the connection's platform override, or the one detected
// from the server's PRDID during the handshake.
func (c *db2Client) Platform() db2Platform {
	return c.platform
}

const (
	db2PackageSectionQuery = 64
	db2PackageSectionSet   = 1
	db2PackageName         = "SYSSH200"
	db2PackageToken        = "SYSLVL01"
	// db2PackageCollectionLUW is a default package DB2 LUW/z-OS always
	// provisions for ad hoc dynamic SQL from unbound/thin clients.
	db2PackageCollectionLUW = "NULLID"
	// db2PackageCollectionIBMi: Db2 for i doesn't ship that NULLID
	// default - confirmed on the real Signature host via
	// QSYS2.SYSPACKAGE (no rows under NULLID at all). QGPL is the
	// library IBM's own ODBC/ACS driver uses by default for the SQL
	// packages it implicitly creates on IBM i, and is present and
	// generally writable on every system, unlike NULLID.
	db2PackageCollectionIBMi = "QGPL"
	// db2PackageNameIBMi: confirmed via QSYS2.SYSPACKAGE on the real
	// Signature host - QGPL/QSQCLIPKGC is IBM i's own pre-bound system
	// CLI package for cursor-based operations (what jt400/ODBC use for
	// queries that return rows). SYSSH200 doesn't exist under QGPL
	// either; this is the actual package, not another ad hoc default.
	db2PackageNameIBMi = "QSQCLIPKGC"
	// db2PackageTokenIBMi: confirmed via QSYS2.SYSPACKAGE on the real
	// Signature host - CONSISTENCY_TOKEN for QGPL/QSQCLIPKGC is hex
	// D8E2D8C3D3C94040, which is cp500 for "QSQCLI" + two trailing
	// spaces (%-8s below pads it the rest of the way). Verified by
	// round-tripping through this file's own cp500 table rather than
	// trusting the translation blind: encodeString("QSQCLI", "cp500")
	// produces that exact byte sequence.
	db2PackageTokenIBMi = "QSQCLI"
	// db2PackageSectionQueryIBMi: switched from 64 to 1 as an experiment
	// while PRPSQLSTT was still failing; confirmed structurally fine by
	// a hex dump of the real wire bytes, so keeping it even though the
	// section number itself was never conclusively the problem.
	db2PackageSectionQueryIBMi = 1
	// db2TypDefNameIBMi: DRDA defines a dedicated TYPDEFNAM for this
	// platform - every other value (QTDSQLX86 etc.) is for other
	// architectures. ACCRDB was sending QTDSQLX86 unconditionally; this
	// is an isolated experiment (not yet confirmed via the real host)
	// after a hex dump ruled out PKGNAMCSN/PRPSQLSTT/SQLSTT structure.
	db2TypDefNameIBMi = "QTDSQL400"
	// db2PRDIDIBMi: ACCRDB's PRDID identifies the requester product and
	// follows a documented vvrrm-style family prefix convention - "QSQ"
	// for Db2 for i itself, "DSN" for z/OS, "SQL" for LUW/CLI/ODBC (what
	// this client sends unconditionally, including against IBM i).
	// Nothing confirms IBM i rejects an "SQL"-family PRDID specifically,
	// but every other field in ACCRDB has now been tried and ruled out
	// (RDBNAM both ways, CRRTKN, TYPDEFNAM, chaining structure) without
	// ACCRDBRM ever arriving, so this is the last untried field.
	db2PRDIDIBMi = "QSQ08060"
)

type db2StreamHandler struct {
	Columns func([]db2Column) error
	Rows    func([][]any) error
}

func dialDb2(ctx context.Context, cfg db2ConnConfig) (*db2Client, error) {
	dialer := net.Dialer{Timeout: cfg.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", cfg.Address)
	if err != nil {
		return nil, err
	}

	if cfg.SSL {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, err
		}
		conn = tlsConn
	}

	private, err := newPrivateKey()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	client := &db2Client{
		conn:     conn,
		host:     cfg.Host,
		ssl:      cfg.SSL,
		database: padDatabaseName(cfg.Database),
		rdbName:  strings.TrimSpace(cfg.RDBName),
		user:     cfg.User,
		password: cfg.Password,
		timeout:  cfg.Timeout,
		private:  private,
		platform: cfg.Platform,
	}
	if err := client.handshake(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return client, nil
}

func (c *db2Client) Query(ctx context.Context, query string) (db2Rows, error) {
	var out db2Rows
	err := c.Stream(ctx, query, db2StreamHandler{
		Columns: func(columns []db2Column) error {
			out.Columns = append(out.Columns[:0], columns...)
			return nil
		},
		Rows: func(rows [][]any) error {
			out.Rows = append(out.Rows, rows...)
			return nil
		},
	})
	return out, err
}

func (c *db2Client) Stream(ctx context.Context, query string, handler db2StreamHandler) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	clearDeadline := c.setDeadline(ctx)
	defer clearDeadline()

	config.Debug("[DB2] Stream: query=%s", query)

	// PRPSQLSTT (prepare) and OPNQRY (open cursor) are each given their own
	// round trip rather than one combined chain, same reasoning as
	// setClientVariables: if IBM i rejects one of these, we want to know
	// which, not an opaque EOF for the whole sequence.
	curID := uint16(1)
	prpSQLSTT, err := c.packPRPSQLSTT()
	if err != nil {
		return err
	}
	sqlSTT, err := packSQLSTT(query)
	if err != nil {
		return err
	}
	config.Debug("[DB2] Stream: sending PRPSQLSTT + SQLSTT")
	config.Debug("[DB2] Stream: PRPSQLSTT wire bytes (object only, no DSS frame): %x", prpSQLSTT)
	config.Debug("[DB2] Stream: SQLSTT wire bytes (object only, no DSS frame): %x", sqlSTT)
	if curID, err = writeRequestDSS(c.conn, prpSQLSTT, curID, true, false); err != nil {
		return fmt.Errorf("sending PRPSQLSTT: %w", err)
	}
	if _, err = writeRequestDSS(c.conn, sqlSTT, curID, false, true); err != nil {
		return fmt.Errorf("sending SQLSTT: %w", err)
	}
	if err := c.parseResponse(ctx, handler); err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}

	opnQRY, err := c.packOPNQRY()
	if err != nil {
		return err
	}
	config.Debug("[DB2] Stream: sending OPNQRY")
	if _, err = writeRequestDSS(c.conn, opnQRY, 1, false, true); err != nil {
		return fmt.Errorf("sending OPNQRY: %w", err)
	}
	if err := c.parseResponse(ctx, handler); err != nil {
		return fmt.Errorf("opening query: %w", err)
	}
	return nil
}

func (c *db2Client) Exec(ctx context.Context, query string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	clearDeadline := c.setDeadline(ctx)
	defer clearDeadline()

	curID := uint16(1)
	var err error
	excSQLIMM, err := c.packEXCSQLIMM()
	if err != nil {
		return err
	}
	if curID, err = writeRequestDSS(c.conn, excSQLIMM, curID, true, false); err != nil {
		return err
	}
	sqlSTT, err := packSQLSTT(query)
	if err != nil {
		return err
	}
	if curID, err = writeRequestDSS(c.conn, sqlSTT, curID, false, false); err != nil {
		return err
	}
	if _, err = writeRequestDSS(c.conn, packRDBCMM(), curID, false, true); err != nil {
		return err
	}
	return c.parseResponse(ctx, db2StreamHandler{})
}

func (c *db2Client) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	clearDeadline := c.setDeadline(ctx)
	defer clearDeadline()
	_, _ = writeRequestDSS(c.conn, packRDBCMM(), 1, false, true)
	_ = c.parseResponse(ctx, db2StreamHandler{})
	err := c.conn.Close()
	c.conn = nil
	return err
}

func (c *db2Client) handshake(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	clearDeadline := c.setDeadline(ctx)
	defer clearDeadline()

	curID := uint16(1)
	var err error
	config.Debug("[DB2] handshake: sending EXCSAT + ACCSEC (secmec=%d)", secmecEncryptedUserPassword)
	if curID, err = writeRequestDSS(c.conn, packEXCSAT(), curID, false, false); err != nil {
		return fmt.Errorf("sending EXCSAT: %w", err)
	}
	if _, err = writeRequestDSS(c.conn, packACCSEC(c.database, secmecEncryptedUserPassword, publicKey(c.private)), curID, false, true); err != nil {
		return fmt.Errorf("sending ACCSEC: %w", err)
	}

	secmec, sectkn, err := c.parseACCSECRD()
	if err != nil {
		return fmt.Errorf("reading ACCSECRD: %w", err)
	}
	config.Debug("[DB2] handshake: ACCSECRD received, secmec=%d, token_len=%d", secmec, len(sectkn))
	resolved := resolveSecMec(secmec, sectkn)

	// A server claiming secmecEncryptedUserPassword with no token, then
	// confirming secmecEUSRIDPWD once we re-propose plain-text auth, is a
	// pattern we've only observed against Db2 for i - it doesn't happen on
	// LUW or z/OS. Used below as a platform-detection fallback for hosts
	// whose ACCRDBRM doesn't carry a PRDID we can match.
	claimedEncryptedNoToken := secmec == secmecEncryptedUserPassword && len(sectkn) == 0
	secmecDowngradedToEUSRIDPWD := false

	if resolved != secmecEncryptedUserPassword {
		// The server couldn't actually back the mechanism it claimed (or
		// claimed none), and we're downgrading locally. DRDA requires
		// re-proposing ACCSEC with the mechanism we're committing to and
		// getting the server's confirmation before SECCHK - some Db2 for i
		// configurations drop the connection if SECCHK arrives with a
		// mechanism that was never explicitly negotiated.
		config.Debug("[DB2] handshake: re-sending ACCSEC with secmec=%d", resolved)
		curID = 1
		if curID, err = writeRequestDSS(c.conn, packACCSEC(c.database, resolved, nil), curID, false, true); err != nil {
			return fmt.Errorf("re-sending ACCSEC with secmec=%d: %w", resolved, err)
		}
		confirmedSecmec, confirmedSectkn, err := c.parseACCSECRD()
		if err != nil {
			return fmt.Errorf("reading confirming ACCSECRD for secmec=%d: %w", resolved, err)
		}
		config.Debug("[DB2] handshake: confirming ACCSECRD received, secmec=%d, token_len=%d", confirmedSecmec, len(confirmedSectkn))
		if confirmedSecmec != 0 {
			resolved = confirmedSecmec
		}
		sectkn = confirmedSectkn
		secmecDowngradedToEUSRIDPWD = claimedEncryptedNoToken && resolved == secmecEUSRIDPWD
	}
	secmec = resolved

	// PRDID (the other platform-detection signal) only arrives in
	// ACCRDBRM, the response to the packet we're about to build - too
	// late to inform it. The SECMEC pattern is the only signal available
	// before ACCRDB is sent, so resolve it now rather than after the
	// round trip, letting ACCRDB declare the right TYPDEFNAM up front
	// instead of the LUW/z-OS default.
	if c.platform == db2PlatformAuto && secmecDowngradedToEUSRIDPWD {
		config.Debug("[DB2] handshake: SECMEC 9->8 downgrade pattern matches IBM i (resolved before ACCRDB, PRDID not available yet)")
		c.platform = db2PlatformIBMi
	}

	// SECMEC 8 (secmecEUSRIDPWD) is a password-substitute mechanism, not
	// plain-text: the real IBM i host on issue #3 returned SECCHKRM
	// SVRCOD=16/SECCHKCD=15 (authentication rejected) for every connection
	// in this investigation because SECCHK was sending plain-text
	// credentials for a mechanism that doesn't accept them. Neither SECMEC
	// 8 nor its substitute-password algorithms are documented in the open
	// DRDA/DDM spec; both are ported from IBM's own JTOpen (jt400) client
	// (see signon.go and substitutePasswordSHA1/512 in drda_crypto.go).
	// Computing one requires a server seed that only the separate Sign-on
	// Server protocol (not DRDA/DDM) provides, so that exchange happens
	// here, once, right before building SECCHK.
	var substituteCredential []byte
	if secmec == secmecEUSRIDPWD {
		signonExchange := c.signonExchange
		if signonExchange == nil {
			signonExchange = exchangeSignonAttributes
		}
		attrs, err := signonExchange(ctx, c.host, c.ssl, c.timeout)
		if err != nil {
			return fmt.Errorf("exchanging Sign-on Server attributes for SECMEC 8: %w", err)
		}
		config.Debug("[DB2] handshake: Sign-on Server passwordLevel=%d client_seed=%x server_seed=%x",
			attrs.passwordLevel, attrs.clientSeed, attrs.serverSeed)
		// IBM i user profile names are always uppercase internally; the
		// substitute algorithms are specified against that canonical form
		// (confirmed via JTOpen's own EBCDIC round-trip, which only
		// recognizes uppercase A-Z in the byte<->char tables it uses here).
		substituteUser := strings.ToUpper(c.user)
		if attrs.passwordLevel >= 4 {
			substituteCredential = substitutePasswordSHA512(substituteUser, c.password, attrs.serverSeed, attrs.clientSeed)
		} else {
			substituteCredential = substitutePasswordSHA1(substituteUser, c.password, attrs.serverSeed, attrs.clientSeed)
		}
		config.Debug("[DB2] handshake: SECCHK PASSWORD field (substitute credential) hex=%x", substituteCredential)
	}

	secchk, err := c.packSECCHK(secmec, sectkn, substituteCredential)
	if err != nil {
		return err
	}

	// CRRTKN correlates this specific unit of work for the server - it must
	// not repeat across unrelated connections. A hardcoded constant was
	// used here previously; a real packet capture against IBM i showed
	// SECCHK (no CRRTKN) gets a reply (SECCHKRM) but the chained ACCRDB
	// (which carries CRRTKN) gets none at all, no error either - exactly
	// the field that differs between the two. Generating a fresh one here
	// is the narrowest fix matching that evidence.
	crrtkn, err := newCorrelationToken()
	if err != nil {
		return fmt.Errorf("generating CRRTKN: %w", err)
	}

	// v22 switched ACCRDB's own RDBNAM to the resolved real RDB name,
	// matching what PKGNAMCSN already needed - confirmed correct on the
	// wire by a packet capture, but ACCRDBRM still never arrived in that
	// round or the next (v23's clean round-trip split). Meanwhile ACCSEC
	// sends the raw *LOCAL alias on both of its round trips (initial and
	// re-negotiated) and gets a reply every time, and so does SECCHK.
	// Reverting ACCRDB specifically back to the raw alias - the one
	// RDBNAM/chaining combination not yet tried - on the theory the
	// server resolves *LOCAL internally for ACCRDB too, the same way it
	// evidently does for ACCSEC/SECCHK. PKGNAMCSN keeps the resolved name
	// unchanged: it's a different request sent later, after a real RDB
	// session would exist, and has no evidence against it either way.
	accrdbDatabase := c.database

	// SECCHK and ACCRDB used to be sent chained in one round trip (corr=1,
	// corr=2). A real packet capture on issue #3 showed the server
	// replying to SECCHK (corr=1) but silently dropping ACCRDB (corr=2) -
	// no error, nothing. That's not actually explained by the differing
	// correlation IDs themselves: EXCSAT/ACCSEC earlier in this same
	// handshake use the identical corr=1/corr=2 chained pattern and both
	// get properly chained replies back. Splitting these into two
	// isolated round trips - same isolation principle already used for
	// PRPSQLSTT/OPNQRY in Stream() and each statement in
	// setClientVariables() - rules DSS chaining in or out conclusively
	// either way, and costs nothing if it turns out not to matter.
	config.Debug("[DB2] handshake: sending SECCHK (secmec=%d)", secmec)
	curID = 1
	if _, err = writeRequestDSS(c.conn, secchk, curID, false, true); err != nil {
		return fmt.Errorf("sending SECCHK: %w", err)
	}
	if err = c.parseResponse(ctx, db2StreamHandler{}); err != nil {
		return fmt.Errorf("reading SECCHKRM: %w", err)
	}

	config.Debug("[DB2] handshake: sending ACCRDB (RDBNAM=%q)", accrdbDatabase)
	if _, err = writeRequestDSS(c.conn, packACCRDB(accrdbDatabase, c.platform, crrtkn), 1, false, true); err != nil {
		return fmt.Errorf("sending ACCRDB: %w", err)
	}
	c.sawACCRDBRM = false
	if err = c.parseResponse(ctx, db2StreamHandler{}); err != nil {
		return fmt.Errorf("reading ACCRDBRM: %w", err)
	}
	if !c.sawACCRDBRM {
		config.Debug("[DB2] handshake: WARNING - no ACCRDBRM was received for ACCRDB; the RDB session is likely not established despite no error")
	}
	config.Debug("[DB2] handshake: SECCHKRM/ACCRDBRM received, authenticated, platform=%s", c.platform)

	if err := c.setClientVariables(ctx); err != nil {
		return fmt.Errorf("setting client variables: %w", err)
	}
	return nil
}

func (c *db2Client) parseACCSECRD() (int, []byte, error) {
	chained := true
	secmec := 0
	var sectkn []byte
	for chained {
		packet, err := readDSS(c.conn)
		if err != nil {
			return 0, nil, err
		}
		chained = packet.chained
		switch packet.codePoint {
		case cpACCSECRD:
			fields := parseReplyObject(packet.object)
			if v := fields[cpSECMEC]; len(v) >= 2 {
				secmec = int(binary.BigEndian.Uint16(v[:2]))
			}
			if v := fields[cpSECTKN]; len(v) > 0 {
				sectkn = append([]byte(nil), v...)
			}
		case cpRDBNFNRM:
			return 0, nil, fmt.Errorf("database not found")
		}
	}
	return secmec, sectkn, nil
}

func (c *db2Client) parseResponse(ctx context.Context, handler db2StreamHandler) error {
	var fields []drdaField
	var errResult error
	chained := true
	needCNTQRY := false
	qryInsID := uint64(0)
	cntCorrelationID := uint16(1)

	for {
		for chained {
			if err := ctx.Err(); err != nil {
				return err
			}
			c.refreshDeadline(ctx)
			packet, err := readDSS(c.conn)
			if err != nil {
				return err
			}
			chained = packet.chained

			for packet.moreData {
				c.refreshDeadline(ctx)
				cntQRY, err := c.packCNTQRY(qryInsID)
				if err != nil {
					return err
				}
				if _, err := writeRequestDSS(c.conn, cntQRY, cntCorrelationID, false, true); err != nil {
					return err
				}
				c.refreshDeadline(ctx)
				extra, err := readDSS(c.conn)
				if err != nil {
					return err
				}
				packet.object = append(packet.object, extra.object...)
				packet.moreData = extra.moreData
				chained = extra.chained
			}

			switch packet.codePoint {
			case cpSQLERRRM:
				if msg := parseDiagnostic(packet.object); msg != "" && errResult == nil {
					errResult = fmt.Errorf("%s", strings.TrimSpace(msg))
				}
			case cpSQLCARD:
				if _, err := parseSQLCard(packet.object); err != nil && errResult == nil {
					errResult = err
				}
			case cpSQLDARD:
				if columns, err := parseSQLDARD(packet.object); err != nil {
					if errResult == nil {
						errResult = err
					}
				} else if len(columns) > 0 {
					if handler.Columns != nil {
						if err := handler.Columns(columns); err != nil {
							return err
						}
					}
				}
			case cpOPNQRYRM:
				needCNTQRY = true
				cntCorrelationID = packet.correlationID
				if v := parseReplyObject(packet.object)[cpQRYINSID]; len(v) >= 8 {
					qryInsID = binary.BigEndian.Uint64(v[:8])
				}
			case cpENDQRYRM:
				needCNTQRY = false
			case cpQRYDSC:
				parsed, err := parseQRYDSC(packet.object)
				if err != nil {
					return err
				}
				fields = parsed
			case cpQRYDTA:
				rows, err := parseQRYDTA(packet.object, fields)
				if err != nil {
					return err
				}
				if len(rows) > 0 && handler.Rows != nil {
					if err := handler.Rows(rows); err != nil {
						return err
					}
				}
			case cpACCRDBRM:
				c.sawACCRDBRM = true
				fields := parseReplyObject(packet.object)
				if c.platform == db2PlatformAuto {
					if v, ok := fields[cpPRDID]; ok && len(v) > 0 {
						prdid := decodeString(v, "cp500")
						detected := detectDb2PlatformFromPRDID(prdid)
						config.Debug("[DB2] handshake: ACCRDBRM PRDID=%q, detected platform=%s", prdid, detected)
						if detected != db2PlatformAuto {
							c.platform = detected
						}
					} else {
						config.Debug("[DB2] handshake: ACCRDBRM has no PRDID, platform stays auto (LUW-shaped catalog queries)")
					}
				}
				// database (e.g. *LOCAL on IBM i) is a symbolic alias that
				// ACCSEC/ACCRDB resolve themselves; PKGNAMCSN needs the real
				// RDB directory name instead. Capture it from ACCRDBRM if
				// the server echoes it back, unless an explicit rdb_name
				// override already took precedence.
				if c.rdbName == "" {
					if v, ok := fields[cpRDBNAM]; ok && len(v) > 0 {
						c.rdbName = strings.TrimSpace(decodeString(v, "cp500"))
						config.Debug("[DB2] handshake: ACCRDBRM RDBNAM=%q, using as the real RDB name for PKGNAMCSN", c.rdbName)
					}
				}
			case cpSECCHKRM:
				if err := parseSECCHKRM(packet.object); err != nil && errResult == nil {
					errResult = err
				}
			case cpEXCSATRD:
			default:
			}
		}

		if needCNTQRY {
			needCNTQRY = false
			c.refreshDeadline(ctx)
			cntQRY, err := c.packCNTQRY(qryInsID)
			if err != nil {
				return err
			}
			if _, err := writeRequestDSS(c.conn, cntQRY, cntCorrelationID, false, true); err != nil {
				return err
			}
			chained = true
			continue
		}
		break
	}

	if errResult != nil {
		return errResult
	}
	return nil
}

// setClientVariables renegotiates the CCSID manager to UTF-8 and sets a
// couple of DB2-LUW-specific SQL special registers (client workstation
// name, locale) that some LUW/z/OS servers expect post-auth. Each piece is
// its own request/reply round trip rather than one combined chain, so a
// statement the server rejects is isolated and reported precisely instead
// of surfacing as an opaque EOF for the whole function.
func (c *db2Client) setClientVariables(ctx context.Context) error {
	if c.platform == db2PlatformIBMi {
		// Everything below is DB2-LUW/z/OS-specific: the post-auth CCSID
		// manager renegotiation (confirmed via a real IBM i host to drop
		// the connection outright - the actual character-encoding
		// negotiation already happened via cpUNICODE in the very first,
		// pre-auth EXCSAT, so this second one is redundant anyway), and
		// SET CLIENT WRKSTNNAME / SET CURRENT LOCALE, which are DB2-LUW
		// SQL special registers IBM i doesn't recognize (workstation name
		// travels in DRDA's own EXCSAT/ACCRDB fields there, and CCSID/
		// locale is a job-level attribute, not a settable SQL register).
		config.Debug("[DB2] setClientVariables: skipping entirely on IBM i")
		return nil
	}

	config.Debug("[DB2] setClientVariables: renegotiating CCSID manager (UTF-8)")
	if err := c.sendAndAwait(ctx, packEXCSATMGRLVLLS()); err != nil {
		return fmt.Errorf("renegotiating CCSID manager: %w", err)
	}

	config.Debug("[DB2] setClientVariables: setting client workstation name")
	if err := c.execSetStatement(ctx, "SET CLIENT WRKSTNNAME 'ingestr'"); err != nil {
		return fmt.Errorf("setting client workstation name: %w", err)
	}

	config.Debug("[DB2] setClientVariables: setting current locale")
	if err := c.execSetStatement(ctx, "SET CURRENT LOCALE LC_CTYPE='en_US'"); err != nil {
		return fmt.Errorf("setting current locale: %w", err)
	}

	config.Debug("[DB2] setClientVariables: committing")
	if err := c.sendAndAwait(ctx, packRDBCMM()); err != nil {
		return fmt.Errorf("committing: %w", err)
	}
	return nil
}

// sendAndAwait sends a single, already-packed DSS object as a complete
// request (not chained to anything else) and reads its reply.
func (c *db2Client) sendAndAwait(ctx context.Context, object []byte) error {
	if _, err := writeRequestDSS(c.conn, object, 1, false, true); err != nil {
		return err
	}
	return c.parseResponse(ctx, db2StreamHandler{})
}

// execSetStatement runs one SET SQL statement as its own request/reply
// round trip.
func (c *db2Client) execSetStatement(ctx context.Context, stmt string) error {
	curID := uint16(1)
	excSQLSET, err := c.packEXCSQLSET()
	if err != nil {
		return err
	}
	if curID, err = writeRequestDSS(c.conn, excSQLSET, curID, true, false); err != nil {
		return err
	}
	sqlSTT, err := packSQLSTT(stmt)
	if err != nil {
		return err
	}
	if _, err = writeRequestDSS(c.conn, sqlSTT, curID, false, true); err != nil {
		return err
	}
	return c.parseResponse(ctx, db2StreamHandler{})
}

func (c *db2Client) setDeadline(ctx context.Context) func() {
	c.refreshDeadline(ctx)
	return func() {
		if c.conn != nil {
			_ = c.conn.SetDeadline(time.Time{})
		}
	}
}

func (c *db2Client) refreshDeadline(ctx context.Context) {
	conn := c.conn
	if conn == nil {
		return
	}
	var deadline time.Time
	if c.timeout > 0 {
		deadline = time.Now().Add(c.timeout)
	}
	if ctxDeadline, ok := ctx.Deadline(); ok && (deadline.IsZero() || ctxDeadline.Before(deadline)) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)
}

// resolveSecMec picks the security mechanism to actually use based on
// whether the server's ACCSECRD reply came with a usable token, not just on
// what SECMEC value it claims. Encrypted auth (SECMEC 9) needs a real
// 32-byte Diffie-Hellman token to encrypt credentials against; without one,
// encryption is impossible regardless of what the server said. Two known
// server behaviors land here:
//   - the server omits SECMEC entirely (secmec == 0) when it only supports
//     plain-text auth, since there's no DH exchange and so no token either
//   - the server echoes back SECMEC 9 (what the client proposed) without
//     ever populating a valid token, because it doesn't actually implement
//     DH-encrypted auth
//
// Both would otherwise reach encryptCredential with a 0-byte (or
// wrong-length) token and fail with "expected 32-byte security token, got
// N". Basing the decision on the token's actual validity, not the claimed
// mechanism, handles both.
func resolveSecMec(secmec int, sectkn []byte) int {
	if secmec == secmecEncryptedUserPassword && len(sectkn) != 32 {
		return secmecUserPassword
	}
	if secmec != 0 {
		return secmec
	}
	if len(sectkn) == 32 {
		return secmecEncryptedUserPassword
	}
	return secmecUserPassword
}

// packSECCHK builds the SECCHK request. substituteCredential is only used
// when secmec == secmecEUSRIDPWD (SECMEC 8): it's the password-substitute
// bytes computed from a Sign-on Server seed exchange (see signon.go and
// substitutePasswordSHA1/512 in drda_crypto.go) - SECMEC 8 reuses the same
// USRID/PASSWORD codepoints as plain-text auth, just with those substitute
// bytes in place of the cp500-encoded password. Callers not using SECMEC 8
// can pass nil.
func (c *db2Client) packSECCHK(secmec int, sectkn []byte, substituteCredential []byte) ([]byte, error) {
	body := bytes.NewBuffer(nil)
	body.Write(packUint(cpSECMEC, secmec, 2))
	rdbName, err := packString(cpRDBNAM, c.database, "cp500")
	if err != nil {
		return nil, fmt.Errorf("failed to encode database name: %w", err)
	}
	body.Write(rdbName)
	switch {
	case secmec == secmecEncryptedUserPassword:
		encodedUser, err := encodeString(c.user, "cp500")
		if err != nil {
			return nil, fmt.Errorf("failed to encode user: %w", err)
		}
		user, err := encryptCredential(sectkn, c.private, encodedUser)
		if err != nil {
			return nil, fmt.Errorf("credential encryption failed for user: %w", err)
		}
		body.Write(packBinary(cpSECTKN, user))

		encodedPassword, err := encodeString(c.password, "cp500")
		if err != nil {
			return nil, fmt.Errorf("failed to encode password: %w", err)
		}
		password, err := encryptCredential(sectkn, c.private, encodedPassword)
		if err != nil {
			return nil, fmt.Errorf("credential encryption failed for password: %w", err)
		}
		body.Write(packBinary(cpSECTKN, password))
	case secmec == secmecEUSRIDPWD:
		user, err := packString(cpUSRID, padUserID(c.user), "cp500")
		if err != nil {
			return nil, fmt.Errorf("failed to encode user: %w", err)
		}
		config.Debug("[DB2] SECCHK: USRID field (cp500, 10 bytes, uppercase-padded) hex=%x", user)
		body.Write(user)
		if len(substituteCredential) == 0 {
			return nil, fmt.Errorf("SECMEC %d requires a password-substitute credential but none was computed", secmec)
		}
		body.Write(packBinary(cpPASSWORD, substituteCredential))
	default:
		user, err := packString(cpUSRID, padUserID(c.user), "cp500")
		if err != nil {
			return nil, fmt.Errorf("failed to encode user: %w", err)
		}
		body.Write(user)
		password, err := packString(cpPASSWORD, c.password, "cp500")
		if err != nil {
			return nil, fmt.Errorf("failed to encode password: %w", err)
		}
		body.Write(password)
	}
	return packDSSObject(cpSECCHK, body.Bytes())
}

// resolvedRDBName returns the real RDB directory name to use wherever a
// DRDA request needs one to actually resolve (ACCRDB, PKGNAMCSN), falling
// back to c.database (commonly a symbolic alias like *LOCAL on IBM i) when
// no override/auto-detected value is available yet. Gated to IBM i since
// that's the only platform where c.rdbName is ever populated.
func (c *db2Client) resolvedRDBName() string {
	if c.platform == db2PlatformIBMi && c.rdbName != "" {
		return padDatabaseName(c.rdbName)
	}
	return c.database
}

func (c *db2Client) packPKGNAMCSN(statementNumber uint16) ([]byte, error) {
	// PKGNAMCSN's RDB/collection/package/token fields are text, same as
	// every other DRDA character field (RDBNAM, PRDID, ...) - all of which
	// are cp500-encoded elsewhere in this file. This one was left as raw
	// ASCII, which a real IBM i host rejects outright (EOF on PRPSQLSTT
	// before it even reaches the SQL text).
	database := c.resolvedRDBName()
	collection, pkgName, token := db2PackageCollectionLUW, db2PackageName, db2PackageToken
	if c.platform == db2PlatformIBMi {
		collection, pkgName, token = db2PackageCollectionIBMi, db2PackageNameIBMi, db2PackageTokenIBMi
		if statementNumber == db2PackageSectionQuery {
			statementNumber = db2PackageSectionQueryIBMi
		}
		if c.rdbName == "" {
			config.Debug("[DB2] PKGNAMCSN: no resolved RDB name on IBM i, falling back to %q (likely wrong if this is a symbolic alias)", c.database)
		}
	}
	text := fmt.Sprintf("%-18s%-18s%-18s%-8s", database, collection, pkgName, token)
	raw, err := encodeString(text, "cp500")
	if err != nil {
		return nil, fmt.Errorf("encoding PKGNAMCSN: %w", err)
	}
	// Log the bytes actually going on the wire (raw, post-cp500), not the
	// pre-encoding Go string - %x on the Go string here would print its
	// ASCII bytes and misleadingly look like the encoding step never ran.
	config.Debug("[DB2] PKGNAMCSN: database=%q collection=%q package=%q token=%q wire_hex=%x section=%d", database, collection, pkgName, token, raw, statementNumber)
	raw = append(raw, byte(statementNumber>>8), byte(statementNumber))
	return packBinary(cpPKGNAMCSN, raw), nil
}

func (c *db2Client) packPRPSQLSTT() ([]byte, error) {
	body := bytes.NewBuffer(nil)
	pkg, err := c.packPKGNAMCSN(db2PackageSectionQuery)
	if err != nil {
		return nil, err
	}
	body.Write(pkg)
	body.Write(packBinary(cpRTNSQLDA, []byte{0xf1}))
	return packDSSObject(cpPRPSQLSTT, body.Bytes())
}

func (c *db2Client) packEXCSQLIMM() ([]byte, error) {
	body := bytes.NewBuffer(nil)
	pkg, err := c.packPKGNAMCSN(db2PackageSectionQuery)
	if err != nil {
		return nil, err
	}
	body.Write(pkg)
	body.Write(packBinary(cpRDBCMTOK, []byte{0xf1}))
	return packDSSObject(cpEXCSQLIMM, body.Bytes())
}

func (c *db2Client) packEXCSQLSET() ([]byte, error) {
	pkg, err := c.packPKGNAMCSN(db2PackageSectionSet)
	if err != nil {
		return nil, err
	}
	return packDSSObject(cpEXCSQLSET, pkg)
}

func (c *db2Client) packOPNQRY() ([]byte, error) {
	body := bytes.NewBuffer(nil)
	pkg, err := c.packPKGNAMCSN(db2PackageSectionQuery)
	if err != nil {
		return nil, err
	}
	body.Write(pkg)
	body.Write(packUint(cpQRYBLKSZ, 65535, 4))
	body.Write(packUint(cpMAXBLKEXT, 65535, 2))
	body.Write(packBinary(cpQRYCLSIMP, []byte{0x01}))
	return packDSSObject(cpOPNQRY, body.Bytes())
}

func (c *db2Client) packCNTQRY(qryInsID uint64) ([]byte, error) {
	body := bytes.NewBuffer(nil)
	pkg, err := c.packPKGNAMCSN(db2PackageSectionQuery)
	if err != nil {
		return nil, err
	}
	body.Write(pkg)
	body.Write(packUint(cpQRYBLKSZ, 65535, 4))
	insID := make([]byte, 8)
	binary.BigEndian.PutUint64(insID, qryInsID)
	body.Write(packBinary(cpQRYINSID, insID))
	body.Write(packBinary(cpRTNEXTDTA, []byte{0x02}))
	return packDSSObject(cpCNTQRY, body.Bytes())
}

func packEXCSAT() []byte {
	body := bytes.NewBuffer(nil)
	body.Write(mustPackString(cpEXTNAM, "ingestr", "cp500"))
	body.Write(mustPackString(cpSRVNAM, "ingestr", "cp500"))
	body.Write(mustPackString(cpSRVRLSLV, "ingestr", "cp500"))
	body.Write(packBinary(cpMGRLVLLS, mgrLevels([]int{
		int(cpAGENT), 10,
		int(cpSQLAM), 11,
		int(cpCMNTCPIP), 5,
		int(cpRDB), 12,
		int(cpSECMGR), 9,
		int(cpUNICODE), 1208,
	})))
	body.Write(mustPackString(cpSRVCLSNM, "ingestr", "cp500"))
	return mustPackDSSObject(cpEXCSAT, body.Bytes())
}

func packEXCSATMGRLVLLS() []byte {
	return mustPackDSSObject(cpEXCSAT, packBinary(cpMGRLVLLS, mgrLevels([]int{int(cpCCSIDMGR), 1208})))
}

func packACCSEC(database string, secmec int, sectkn []byte) []byte {
	body := bytes.NewBuffer(nil)
	body.Write(packUint(cpSECMEC, secmec, 2))
	body.Write(mustPackString(cpRDBNAM, database, "cp500"))
	if len(sectkn) > 0 {
		body.Write(packBinary(cpSECTKN, sectkn))
	}
	return mustPackDSSObject(cpACCSEC, body.Bytes())
}

func packACCRDB(database string, platform db2Platform, crrtkn []byte) []byte {
	typdefnam := "QTDSQLX86"
	prdid := "SQL11014"
	if platform == db2PlatformIBMi {
		typdefnam = db2TypDefNameIBMi
		prdid = db2PRDIDIBMi
	}
	body := bytes.NewBuffer(nil)
	body.Write(mustPackString(cpRDBNAM, database, "cp500"))
	body.Write(packUint(cpRDBACCCL, int(cpSQLAM), 2))
	body.Write(mustPackString(cpPRDID, prdid, "cp500"))
	body.Write(mustPackString(cpTYPDEFNAM, typdefnam, "cp500"))
	body.Write(packBinary(cpCRRTKN, crrtkn))
	body.Write(packBinary(cpTYPDEFOVR, mustHex("0006119c04b80006119d04b00006119e04b8")))
	return mustPackDSSObject(cpACCRDB, body.Bytes())
}

func packSQLSTT(sql string) ([]byte, error) {
	body := bytes.NewBuffer(nil)
	body.Write(packNullString(sql))
	body.Write([]byte{0xff})
	return packDSSObject(cpSQLSTT, body.Bytes())
}

func packRDBCMM() []byte {
	return mustPackDSSObject(cpRDBCMM, nil)
}

func mgrLevels(values []int) []byte {
	out := make([]byte, len(values)*2)
	for i, v := range values {
		binary.BigEndian.PutUint16(out[i*2:i*2+2], uint16(v))
	}
	return out
}

func padDatabaseName(database string) string {
	db := strings.ToUpper(database)
	if len(db) >= 18 {
		return db[:18]
	}
	return db + strings.Repeat(" ", 18-len(db))
}

// padUserID uppercases and blank-pads (or truncates) a user ID to exactly
// 10 characters - the fixed width IBM's own JTOpen client always sends on
// the wire for SECCHK's USRID field (DDMSECCHKRequestDataStream always
// copies exactly 10 bytes there, regardless of which security mechanism is
// in play), confirmed directly from that source while investigating why
// SECMEC 8 substitute-password auth (added in v27) was still rejected.
// packString encodes whatever length it's given with no padding, so a
// user ID shorter than 10 characters was going out undersized.
func padUserID(user string) string {
	u := strings.ToUpper(user)
	if len(u) >= 10 {
		return u[:10]
	}
	return u + strings.Repeat(" ", 10-len(u))
}

func mustPackString(codePoint uint16, value string, encoding string) []byte {
	out, err := packString(codePoint, value, encoding)
	if err != nil {
		panic(err)
	}
	return out
}

func mustHex(value string) []byte {
	out, err := hex.DecodeString(value)
	if err != nil {
		panic(err)
	}
	return out
}
