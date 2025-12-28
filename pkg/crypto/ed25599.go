package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
)

func ed25519Ex() {
	// 1. 待签名的原始数据
	originalData := []byte("这是需要进行签名验签的原始数据：Hello Ed25519!")
	fmt.Printf("原始数据：%s\n", string(originalData))
	fmt.Printf("原始数据十六进制：%s\n\n", hex.EncodeToString(originalData))

	// 2. 生成Ed25519密钥对（公钥+私钥）
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("公钥（十六进制）：%s\n", hex.EncodeToString(publicKey))
	fmt.Printf("私钥（十六进制）：%s\n\n", hex.EncodeToString(privateKey))

	// 3. 私钥签名
	signature := ed25519.Sign(privateKey, originalData)
	fmt.Printf("签名结果（十六进制）：%s\n\n", hex.EncodeToString(signature))

	// 4. 公钥验签（正常场景：数据未篡改）
	verifyResult := ed25519.Verify(publicKey, originalData, signature)
	fmt.Printf("验签结果（数据未篡改）：%t\n", verifyResult)

	// 5. 模拟数据篡改后的验签（验证有效性）
	modifiedData := []byte("这是被篡改的数据：Hello Ed25519!")
	verifyResultAfterModify := ed25519.Verify(publicKey, modifiedData, signature)
	fmt.Printf("验签结果（数据已篡改）：%t\n", verifyResultAfterModify)
}

/*
ed25519是一个非对称加密的签名方法，它非常快、非常安全、产生的数据也非常小巧
它的签名长度为64个字节，公钥长度是32个字节

签名一共经过三个环节

    生成公钥、私钥
    使用私钥签名
    使用公钥验签

*/
