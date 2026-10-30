package db2

import (
	"fmt"
	"strings"
)

// cp37Decode is IBM i's job CCSID 37 (US/Canada EBCDIC). A real IBM i host
// returns character data in this encoding regardless of what ACCRDB's
// TYPDEFOVR requests (confirmed on issue #3: SYSCOLUMNS/SYSTABLES text
// came back as EBCDIC even though TYPDEFOVR asks for CCSID 1208/UTF-8).
// CCSID 37 and CCSID 500 (cp500, used elsewhere in this file for DRDA
// protocol fields like RDBNAM/USRID/PASSWORD) share the same repertoire
// but differ at exactly 7 byte positions - confirmed against IBM's own
// CCSID 37 vs 500 comparison, not guessed. Derived from cp500Decode with
// those 7 positions overridden, rather than transcribed by hand, so the
// only novel part of this table is the documented delta itself.
var cp37Decode = func() [256]rune {
	table := cp500Decode
	overrides := map[byte]rune{
		0x4A: 0x00A2, // ¢ (cp500: '[')
		0x4F: 0x007C, // |  (cp500: '!')
		0x5A: 0x0021, // !  (cp500: ']')
		0x5F: 0x00AC, // ¬  (cp500: '^')
		0xB0: 0x005E, // ^  (cp500: '¢')
		0xBA: 0x005B, // [  (cp500: '¬')
		0xBB: 0x005D, // ]  (cp500: '|')
	}
	for b, r := range overrides {
		table[b] = r
	}
	return table
}()

var cp37Encode = func() map[rune]byte {
	encoded := make(map[rune]byte, len(cp37Decode))
	for i, r := range cp37Decode {
		if _, exists := encoded[r]; !exists {
			encoded[r] = byte(i)
		}
	}
	return encoded
}()

func encodeCP37(value string) ([]byte, error) {
	out := make([]byte, 0, len(value))
	for _, r := range value {
		b, ok := cp37Encode[r]
		if !ok {
			return nil, fmt.Errorf("cp37 cannot encode %q", r)
		}
		out = append(out, b)
	}
	return out, nil
}

func decodeCP37(value []byte) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, v := range value {
		b.WriteRune(cp37Decode[v])
	}
	return b.String()
}
