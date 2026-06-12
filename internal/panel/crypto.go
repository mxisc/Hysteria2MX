package panel

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

const encryptedPrefix = "enc:"

func EncryptValue(plainText string, cfg Config) (string, error) {
	if plainText == "" {
		return "", nil
	}
	if strings.HasPrefix(plainText, encryptedPrefix) {
		return plainText, nil
	}

	key := deriveEncryptionKey(cfg.EncryptionKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}

	payload := gcm.Seal(nil, nonce, []byte(plainText), nil)
	return encryptedPrefix + base64.StdEncoding.EncodeToString(append(nonce, payload...)), nil
}

func DecryptValue(cipherText string, cfg Config) (string, error) {
	if cipherText == "" || !strings.HasPrefix(cipherText, encryptedPrefix) {
		return cipherText, nil
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(cipherText, encryptedPrefix))
	if err != nil {
		return "", errors.New("敏感信息解密失败")
	}

	key := deriveEncryptionKey(cfg.EncryptionKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", errors.New("敏感信息解密失败")
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", errors.New("敏感信息解密失败")
	}

	if len(raw) < gcm.NonceSize() {
		return "", errors.New("敏感信息解密失败")
	}

	nonce := raw[:gcm.NonceSize()]
	payload := raw[gcm.NonceSize():]
	plainText, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", errors.New("敏感信息解密失败")
	}

	return string(plainText), nil
}

func DecryptLoginPassword(cipherHex string, challengeNonce string, cfg Config) (string, error) {
	if cipherHex == "" {
		return "", nil
	}
	if len(cipherHex)%2 != 0 {
		return "", errors.New("登录密码格式无效")
	}

	cipherBytes, err := hex.DecodeString(cipherHex)
	if err != nil {
		return "", errors.New("登录密码格式无效")
	}

	keyHex := sha256HexToMD5Hex(DeriveLoginChallengeSeed(challengeNonce, cfg))
	ivHex := sha1HexPrefix(keyHex, 16)

	block, err := aes.NewCipher([]byte(keyHex))
	if err != nil {
		return "", errors.New("密码校验失败")
	}
	if len(cipherBytes)%aes.BlockSize != 0 {
		return "", errors.New("密码校验失败")
	}

	mode := cipher.NewCBCDecrypter(block, []byte(ivHex))
	plain := make([]byte, len(cipherBytes))
	mode.CryptBlocks(plain, cipherBytes)
	plain, err = pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return "", errors.New("密码校验失败")
	}

	return string(plain), nil
}

func DeriveLoginChallengeSeed(challengeNonce string, cfg Config) string {
	seed := sha256.Sum256([]byte(cfg.LoginAESSeed + ":" + challengeNonce))
	return hex.EncodeToString(seed[:])
}

func deriveEncryptionKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func sha256HexToMD5Hex(source string) string {
	return md5SumHex(source)
}

func sha1HexPrefix(value string, length int) string {
	sum := sha1SumHex(value)
	if length > len(sum) {
		length = len(sum)
	}
	return sum[:length]
}

func md5SumHex(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func sha1SumHex(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("invalid padding size")
	}

	paddingLength := int(data[len(data)-1])
	if paddingLength == 0 || paddingLength > blockSize || paddingLength > len(data) {
		return nil, errors.New("invalid padding value")
	}

	for _, value := range data[len(data)-paddingLength:] {
		if int(value) != paddingLength {
			return nil, errors.New("invalid padding bytes")
		}
	}

	return data[:len(data)-paddingLength], nil
}
