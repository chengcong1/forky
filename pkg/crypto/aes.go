package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// ###################### AES加密 ########################

// 长度32字节(256比特)的切片长度 对应AES-256，密钥16字节对用AES-128，密钥24字节对用AES-192，
func GenerateAESKey(num int) ([]byte, error) {
	key := make([]byte, num)
	// 从系统的随机源中读取随机数据填充到 key 中
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("生成随机AES密钥失败: %v", err)
	}
	return key, nil
}

// 实现AES-GCM加密
func AesEncrypt(plainText []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("创建加密块失败: %v", err)
	}
	gcm, err := cipher.NewGCM(block) // 使用GCM模式（Galois/Counter Mode）
	if err != nil {
		return nil, fmt.Errorf("创建GCM模式失败: %v", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("生成随机数失败: %v", err)
	}
	return gcm.Seal(nonce, nonce, plainText, nil), nil
}

// 实现AES-GCM解密
func AesDecrypt(cipherText []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("创建解密块失败: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("创建GCM模式失败: %v", err)
	}
	nonceSize := gcm.NonceSize()
	if len(cipherText) < nonceSize {
		return nil, fmt.Errorf("密文长度异常")
	}
	nonce, cipherText := cipherText[:nonceSize], cipherText[nonceSize:]
	return gcm.Open(nil, nonce, cipherText, nil)
}

// 实现AES-CBC加密
func AesEncryptCBC(origData []byte, key []byte) ([]byte, error) {
	//创建加密算法实例
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	//获取块的大小
	blockSize := block.BlockSize()
	//对数据进行填充，让数据长度满足需求
	origData = pkcs7Padding(origData, blockSize)
	//采用AES加密方法中CBC加密模式
	blocMode := cipher.NewCBCEncrypter(block, key[:blockSize])
	crypted := make([]byte, len(origData))
	//执行加密
	blocMode.CryptBlocks(crypted, origData)
	return crypted, nil
}

// PKCS7 填充模式
func pkcs7Padding(ciphertext []byte, blockSize int) []byte {
	padding := blockSize - len(ciphertext)%blockSize
	//Repeat()函数的功能是把切片[]byte{byte(padding)}复制padding个，然后合并成新的字节切片返回
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(ciphertext, padtext...)
}

// 实现AES-CBC解密
func AesDeCrypt(cypted []byte, key []byte) ([]byte, error) {
	//创建加密算法实例
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	//获取块大小
	blockSize := block.BlockSize()
	//创建加密客户端实例
	blockMode := cipher.NewCBCDecrypter(block, key[:blockSize])
	origData := make([]byte, len(cypted))
	//这个函数也可以用来解密
	blockMode.CryptBlocks(origData, cypted)
	//去除填充字符串
	origData, err = pkcs7UnPadding(origData)
	if err != nil {
		return nil, err
	}
	return origData, err
}

// 填充的反向操作，删除填充字符串
func pkcs7UnPadding(origData []byte) ([]byte, error) {
	//获取数据长度
	length := len(origData)
	if length == 0 {
		return nil, errors.New("加密字符串错误！")
	} else {
		//获取填充字符串长度
		unpadding := int(origData[length-1])
		//截取切片，删除填充字节，并且返回明文
		return origData[:(length - unpadding)], nil
	}
}

// 哈希算法应用
// 使用SHA256验证数据完整性
// func hashData(data []byte) string {
//     hasher := sha256.New()
//     hasher.Write(data)
//     return hex.EncodeToString(hasher.Sum(nil))
// }

// // 示例用法
// originalHash := hashData([]byte("重要数据"))
// // 传输后验证
// if hashData(receivedData) != originalHash {
//     fmt.Println("数据已被篡改！")
// }
