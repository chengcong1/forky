package crypto

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

/*
1、GetStrSha256(s string) string 得到字符串的sha256
2、GetStrSha512(s string) string 得到字符串的sha512
*/
func GetStrSha256(s string) string {
	h := sha256.New()  // 创建一个新的哈希器
	h.Write([]byte(s)) // 写入数据
	// hash := hasher.Sum(nil) // 获取哈希值的字节切片
	return hex.EncodeToString(h.Sum(nil))
}

func GetStrSha512(s string) string {
	h := sha512.New()  // 创建一个新的哈希器
	h.Write([]byte(s)) // 写入数据
	// hash := hasher.Sum(nil) // 获取哈希值的字节切片
	return hex.EncodeToString(h.Sum(nil))
}

// 字符串 SHA-256 的校验
func GetSHA256FromString(data string) string {
	h := sha256.New()
	io.WriteString(h, data)
	return fmt.Sprintf("%x", data)
}

// 文件的 SHA256
func GetSHA256FromFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	sum := fmt.Sprintf("%x", h.Sum(nil))
	return sum, nil
}

// 字符串 md5
func GetMd5FromString(data string) string {
	h := md5.New()
	io.WriteString(h, data)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func GetMd5FromString2(passwd string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(passwd)))
}

// 获取文件的 md5
func GetMd5FromFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
