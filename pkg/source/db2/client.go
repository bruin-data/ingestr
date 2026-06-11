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
)

type db2Client struct {
	conn     net.Conn
	database string
	user     string
	password string
	timeout  time.Duration
	private  *big.Int
	mu       sync.Mutex
}

const (
	db2PackageSectionQuery = 64
	db2PackageSectionSet   = 1
	db2PackageName         = "SYSSH200"
	db2PackageToken        = "SYSLVL01"
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
		database: padDatabaseName(cfg.Database),
		user:     cfg.User,
		password: cfg.Password,
		timeout:  cfg.Timeout,
		private:  private,
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

	curID := uint16(1)
	var err error
	prpSQLSTT, err := c.packPRPSQLSTT()
	if err != nil {
		return err
	}
	if curID, err = writeRequestDSS(c.conn, prpSQLSTT, curID, true, false); err != nil {
		return err
	}
	if curID, err = writeRequestDSS(c.conn, packSQLSTT(query), curID, false, false); err != nil {
		return err
	}
	opnQRY, err := c.packOPNQRY()
	if err != nil {
		return err
	}
	if _, err = writeRequestDSS(c.conn, opnQRY, curID, false, true); err != nil {
		return err
	}
	return c.parseResponse(ctx, handler)
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
	if curID, err = writeRequestDSS(c.conn, packSQLSTT(query), curID, false, false); err != nil {
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
	if curID, err = writeRequestDSS(c.conn, packEXCSAT(), curID, false, false); err != nil {
		return err
	}
	if _, err = writeRequestDSS(c.conn, packACCSEC(c.database, secmecEncryptedUserPassword, publicKey(c.private)), curID, false, true); err != nil {
		return err
	}

	secmec, sectkn, err := c.parseACCSECRD()
	if err != nil {
		return err
	}
	if secmec == 0 {
		secmec = secmecEncryptedUserPassword
	}

	secchk, err := c.packSECCHK(secmec, sectkn)
	if err != nil {
		return err
	}

	curID = 1
	if curID, err = writeRequestDSS(c.conn, secchk, curID, false, false); err != nil {
		return err
	}
	if _, err = writeRequestDSS(c.conn, packACCRDB(c.database), curID, false, true); err != nil {
		return err
	}
	if err = c.parseResponse(ctx, db2StreamHandler{}); err != nil {
		return err
	}

	return c.setClientVariables(ctx)
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
			case cpSECCHKRM, cpACCRDBRM, cpEXCSATRD:
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

func (c *db2Client) setClientVariables(ctx context.Context) error {
	curID := uint16(1)
	var err error
	if curID, err = writeRequestDSS(c.conn, packEXCSATMGRLVLLS(), curID, false, false); err != nil {
		return err
	}
	excSQLSET, err := c.packEXCSQLSET()
	if err != nil {
		return err
	}
	if curID, err = writeRequestDSS(c.conn, excSQLSET, curID, true, false); err != nil {
		return err
	}
	if curID, err = writeRequestDSS(c.conn, packSQLSTT("SET CLIENT WRKSTNNAME 'ingestr'"), curID, true, false); err != nil {
		return err
	}
	if curID, err = writeRequestDSS(c.conn, packSQLSTT("SET CURRENT LOCALE LC_CTYPE='en_US'"), curID, false, false); err != nil {
		return err
	}
	if _, err = writeRequestDSS(c.conn, packRDBCMM(), curID, false, true); err != nil {
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

func (c *db2Client) packSECCHK(secmec int, sectkn []byte) ([]byte, error) {
	body := bytes.NewBuffer(nil)
	body.Write(packUint(cpSECMEC, secmec, 2))
	rdbName, err := packString(cpRDBNAM, c.database, "cp500")
	if err != nil {
		return nil, fmt.Errorf("failed to encode database name: %w", err)
	}
	body.Write(rdbName)
	if secmec == secmecEncryptedUserPassword {
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
	} else {
		user, err := packString(cpUSRID, c.user, "cp500")
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
	return packDSSObject(cpSECCHK, body.Bytes()), nil
}

func (c *db2Client) packPKGNAMCSN(statementNumber uint16) ([]byte, error) {
	// PKGNAMCSN names pre-bound CLI packages by their catalog bytes; the consistency token is binary.
	raw := []byte(fmt.Sprintf("%-18s%-18s%-18s%-8s", c.database, "NULLID", db2PackageName, db2PackageToken))
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
	return packDSSObject(cpPRPSQLSTT, body.Bytes()), nil
}

func (c *db2Client) packEXCSQLIMM() ([]byte, error) {
	body := bytes.NewBuffer(nil)
	pkg, err := c.packPKGNAMCSN(db2PackageSectionQuery)
	if err != nil {
		return nil, err
	}
	body.Write(pkg)
	body.Write(packBinary(cpRDBCMTOK, []byte{0xf1}))
	return packDSSObject(cpEXCSQLIMM, body.Bytes()), nil
}

func (c *db2Client) packEXCSQLSET() ([]byte, error) {
	pkg, err := c.packPKGNAMCSN(db2PackageSectionSet)
	if err != nil {
		return nil, err
	}
	return packDSSObject(cpEXCSQLSET, pkg), nil
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
	return packDSSObject(cpOPNQRY, body.Bytes()), nil
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
	return packDSSObject(cpCNTQRY, body.Bytes()), nil
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
	return packDSSObject(cpEXCSAT, body.Bytes())
}

func packEXCSATMGRLVLLS() []byte {
	return packDSSObject(cpEXCSAT, packBinary(cpMGRLVLLS, mgrLevels([]int{int(cpCCSIDMGR), 1208})))
}

func packACCSEC(database string, secmec int, sectkn []byte) []byte {
	body := bytes.NewBuffer(nil)
	body.Write(packUint(cpSECMEC, secmec, 2))
	body.Write(mustPackString(cpRDBNAM, database, "cp500"))
	if len(sectkn) > 0 {
		body.Write(packBinary(cpSECTKN, sectkn))
	}
	return packDSSObject(cpACCSEC, body.Bytes())
}

func packACCRDB(database string) []byte {
	body := bytes.NewBuffer(nil)
	body.Write(mustPackString(cpRDBNAM, database, "cp500"))
	body.Write(packUint(cpRDBACCCL, int(cpSQLAM), 2))
	body.Write(mustPackString(cpPRDID, "SQL11014", "cp500"))
	body.Write(mustPackString(cpTYPDEFNAM, "QTDSQLX86", "cp500"))
	body.Write(packBinary(cpCRRTKN, mustHex("d5c6f0f0f0f0f0f12ec3f0c1f50155630d5a11")))
	body.Write(packBinary(cpTYPDEFOVR, mustHex("0006119c04b80006119d04b00006119e04b8")))
	return packDSSObject(cpACCRDB, body.Bytes())
}

func packSQLSTT(sql string) []byte {
	body := bytes.NewBuffer(nil)
	body.Write(packNullString(sql))
	body.Write([]byte{0xff})
	return packDSSObject(cpSQLSTT, body.Bytes())
}

func packRDBCMM() []byte {
	return packDSSObject(cpRDBCMM, nil)
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
