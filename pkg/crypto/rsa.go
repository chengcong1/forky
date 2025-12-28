package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"fmt"
)

// ###################### RSA加密 ########################

// 生成密钥对
func generateRSAKeys() (*rsa.PrivateKey, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("生成密钥失败: %v", err)
	}
	return privateKey, nil
}

// RSA加密实现
func rsaEncrypt(plainText []byte, publicKey *rsa.PublicKey) ([]byte, error) {
	// 使用OAEP填充方案
	cipherText, err := rsa.EncryptOAEP(
		sha256.New(),
		rand.Reader,
		publicKey,
		plainText,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("RSA加密失败: %v", err)
	}
	return cipherText, nil
}

// RSA解密实现
func rsaDecrypt(cipherText []byte, privateKey *rsa.PrivateKey) ([]byte, error) {
	plainText, err := rsa.DecryptOAEP(
		sha256.New(),
		rand.Reader,
		privateKey,
		cipherText,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("RSA解密失败: %v", err)
	}
	return plainText, nil
}
