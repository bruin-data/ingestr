package db2

import (
	"bytes"
	"crypto/cipher"
	"crypto/des"
	"crypto/rand"
	"fmt"
	"math/big"
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
