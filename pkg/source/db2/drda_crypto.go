package db2

import (
	"bytes"
	"crypto/cipher"
	"crypto/des"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // required by IBM i's own SECMEC 8 substitute-password algorithm, not a choice
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"math/big"
	"strings"
	"unicode/utf16"

	"golang.org/x/crypto/pbkdf2"

	"github.com/bruin-data/ingestr/internal/config"
)

var (
	dhPrime, _ = new(big.Int).SetString("C62112D73EE613F0947AB31F0F6846A1BFF5B3A4CA0D60BC1E4C7A0D8C16B3E3", 16)
	dhBase, _  = new(big.Int).SetString("4690FA1F7B9E1D4442C86C9114603FDECF071EDCEC5F626E21E256AED9EA34E4", 16)
)

func newPrivateKey() (*big.Int, error) {
	max := new(big.Int).Sub(dhPrime, big.NewInt(2))
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return nil, err
	}
	return n.Add(n, big.NewInt(2)), nil
}

// newCorrelationToken returns a connection-unique DRDA correlation token
// (CRRTKN), sent on ACCRDB. The DDM spec defines CRRTKN as correlating the
// processing of one particular unit of work between requester and server -
// it must not be the same value across unrelated connections. 8 bytes
// keeps it comfortably under the 12-byte CRRTKN limit documented for some
// Db2 server versions (longer tokens have been observed to break the
// connection outright on those).
func newCorrelationToken() ([]byte, error) {
	token := make([]byte, 8)
	if _, err := rand.Read(token); err != nil {
		return nil, fmt.Errorf("generating CRRTKN: %w", err)
	}
	return token, nil
}

// utf16BE encodes a string as big-endian UTF-16 bytes (no BOM) - the
// "Unicode" encoding IBM i's own password-substitute algorithms are
// specified against (CCSID 13488), distinct from both cp500/EBCDIC and
// UTF-8 used elsewhere in this DRDA client.
func utf16BE(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 0, len(units)*2)
	for _, u := range units {
		out = append(out, byte(u>>8), byte(u))
	}
	return out
}

// utf16BEBlankPadded right-pads (or truncates) s to exactly width runes
// with ASCII spaces before UTF-16BE encoding it. Both password-substitute
// algorithms below operate on a fixed-width, blank-padded user ID this way.
func utf16BEBlankPadded(s string, width int) []byte {
	runes := []rune(s)
	if len(runes) > width {
		runes = runes[:width]
	}
	for len(runes) < width {
		runes = append(runes, ' ')
	}
	return utf16BE(string(runes))
}

// substitutePasswordSequence is DRDA/DDM's PWSEQs - an 8-byte sequence
// number both substitute-password algorithms below hash in. IBM i's host
// servers always use a sequence number of 1 regardless of what a client
// sends, per IBM's own implementation (see the JTOpen source cited below);
// there's nothing to negotiate so this is a fixed value, not a counter.
var substitutePasswordSequence = []byte{0, 0, 0, 0, 0, 0, 0, 1}

// substitutePasswordSHA1 computes the IBM i password-substitute credential
// used under DRDA SECMEC 8 when the Sign-on Server reports password level
// 2 or 3. Neither this algorithm nor SECMEC 8 itself is documented in the
// open DRDA/DDM specification; ported byte-for-byte from IBM's own JTOpen
// (jt400) client - AS400ImplRemote.generateShaToken/generateShaSubstitute,
// commit 613335c, src/main/java/com/ibm/as400/access/AS400ImplRemote.java.
// serverSeed/clientSeed come from the Sign-on Server's attribute-exchange
// reply (see signon.go) - SECCHK cannot carry a SECMEC 8 credential without
// that exchange happening first.
func substitutePasswordSHA1(userID, password string, serverSeed, clientSeed []byte) []byte {
	// Despite how it's commonly described (including in IBM's own doc
	// comments elsewhere), this is NOT EBCDIC: AS400ImplRemote feeds
	// generateShaToken/generateShaSubstitute UTF-16BE ("Unicode") bytes -
	// confirmed directly from source (BinaryConverter.charArrayToByteArray
	// after decoding the EBCDIC user ID back to Unicode chars). Logged as
	// what it actually is, not the commonly assumed EBCDIC form, since that
	// mismatch is exactly the kind of thing worth being able to see.
	userIDBytes := utf16BEBlankPadded(userID, 10)
	passwordBytes := utf16BE(strings.TrimRight(password, "\x00 　"))

	tokenHash := sha1.New() //nolint:gosec
	tokenHash.Write(userIDBytes)
	tokenHash.Write(passwordBytes)
	token := tokenHash.Sum(nil)

	substituteHash := sha1.New() //nolint:gosec
	substituteHash.Write(token)
	substituteHash.Write(serverSeed)
	substituteHash.Write(clientSeed)
	substituteHash.Write(userIDBytes)
	substituteHash.Write(substitutePasswordSequence)
	substitute := substituteHash.Sum(nil)

	config.Debug("[DB2] substitutePasswordSHA1: user_utf16be=%x password_utf16be=%x server_seed=%x client_seed=%x sequence=%x",
		userIDBytes, passwordBytes, serverSeed, clientSeed, substitutePasswordSequence)
	config.Debug("[DB2] substitutePasswordSHA1: token(sha1 of user_utf16be+password_utf16be)=%x", token)
	config.Debug("[DB2] substitutePasswordSHA1: substitute(sha1 of token+server_seed+client_seed+user_utf16be+sequence)=%x", substitute)
	return substitute
}

// passwordLevel4Salt builds the 32-byte salt IBM i's PBKDF2 password-level-4
// substitute is keyed with: SHA-256 of a 28-byte buffer holding the
// 10-char blank-padded user ID followed by the last 4 characters of the
// password (blank-padded to 4 if shorter), all UTF-16BE. Ported from
// AS400ImplRemote.generateSaltForPasswordLevel4.
func passwordLevel4Salt(userID, password string) []byte {
	runes := []rune(password)
	tail := runes
	if len(tail) > 4 {
		tail = tail[len(tail)-4:]
	}
	buf := make([]byte, 0, 28)
	buf = append(buf, utf16BEBlankPadded(userID, 10)...)
	buf = append(buf, utf16BEBlankPadded(string(tail), 4)...)
	sum := sha256.Sum256(buf)
	return sum[:]
}

// substitutePasswordSHA512 computes the IBM i password-substitute credential
// used under DRDA SECMEC 8 when the Sign-on Server reports password level 4
// or higher. Ported byte-for-byte from IBM's own JTOpen (jt400) client -
// AS400ImplRemote.generatePwdTokenForPasswordLevel4/
// generateSha512Substitute, same commit as substitutePasswordSHA1. IBM's
// own comment in that source attributes the algorithm to "the security
// team" with the iteration count (10022) and PBKDF2/HMAC-SHA-512 choice
// fixed by that spec, not a tunable.
func substitutePasswordSHA512(userID, password string, serverSeed, clientSeed []byte) []byte {
	salt := passwordLevel4Salt(userID, password)
	token := pbkdf2.Key(utf16BE(password), salt, 10022, 64, sha512.New)

	h := sha512.New()
	h.Write(token)
	h.Write(serverSeed)
	h.Write(clientSeed)
	h.Write(utf16BEBlankPadded(userID, 10))
	h.Write(substitutePasswordSequence)
	return h.Sum(nil)
}

func publicKey(private *big.Int) []byte {
	v := new(big.Int).Exp(dhBase, private, dhPrime).Bytes()
	if len(v) >= 32 {
		return v[len(v)-32:]
	}
	return append(bytes.Repeat([]byte{0}, 32-len(v)), v...)
}

func encryptCredential(serverToken []byte, private *big.Int, plaintext []byte) ([]byte, error) {
	if len(serverToken) != 32 {
		return nil, fmt.Errorf("expected 32-byte security token, got %d", len(serverToken))
	}
	serverPublic := new(big.Int).SetBytes(serverToken)
	session := new(big.Int).Exp(serverPublic, private, dhPrime).Bytes()
	if len(session) < 32 {
		session = append(bytes.Repeat([]byte{0}, 32-len(session)), session...)
	}

	key := session[12:20]
	// DRDA encrypted user/password authentication requires DES-CBC and derives
	// the IV from the server token. Use TLS when server authenticity matters.
	iv := serverToken[12:20]

	block, err := des.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padded := pkcs5Pad(plaintext, block.BlockSize())
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return out, nil
}

func pkcs5Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	out := make([]byte, len(data)+padding)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(padding)
	}
	return out
}
