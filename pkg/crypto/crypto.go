package crypto

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"github.com/cespare/xxhash/v2"
	"golang.org/x/crypto/bcrypt"
)

/*
	文件内包含的函数以及简短的说明

9、GetPasswdWithBcrypt(password string) (string, error) 生成 bcrypt 哈希密码
10、CompareBcryptPasswd(password, storedHash string) bool  验证bcrypt加密的密码
*/

func SliceItemSwap(s []byte, i, j int) {
	s[i], s[j] = s[j], s[i]
}

// 高级加密标准（Adevanced Encryption Standard ,AES）

var PwdKey = []byte("2621609520@qq.com012345678901234")

// 16,24,32位字符串的话，分别对应AES-128，AES-192，AES-256 加密方法
// key不能泄露

// 加密base64(使用自定义Key)
func EnPwdCodeUseKey(pwd []byte, pwdKey []byte) (string, error) {
	result, err := AesEncryptGCM(pwd, pwdKey)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(result), err
}

// 解密(自定义key)
func DePwdCodeUseKey(pwd string, pwdKey []byte) ([]byte, error) {
	//解密base64字符串
	pwdByte, err := base64.StdEncoding.DecodeString(pwd)
	if err != nil {
		return nil, err
	}
	//执行AES解密
	return AesDecryptGCM(pwdByte, pwdKey)
}

// CompareBcryptPasswd 验证bcrypt加密的密码
func CompareBcryptPasswd(password, storedHash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password))
	return err == nil
}

// GetPasswdWithBcrypt 得到由bcrypt加密的密码
func GetPasswdWithBcrypt(password string) (string, error) {
	// 注意：bcrypt 自动管理内部盐（无需单独存储外部盐）
	hashedBytes, err := bcrypt.GenerateFromPassword(
		[]byte(password),
		bcrypt.DefaultCost, // 建议设置 Cost >= 12  DefaultCost int = 10
	)
	return string(hashedBytes), err
}

// 处理加密解密的Key，key 必须是16，24，32位字符
// func handleKey(pwdKey string) []byte {
// 	key := []byte(pwdKey)
// 	l := len(key)
// 	if l != 16 && l != 24 && l != 32 {
// 		fmt.Println("输入的key错误：key为16，24，32位字符")
// 		os.Exit(0)
// 	}
// 	return key
// }

// CalculateFileHash 计算文件的 xxhash 哈希值
func CalculateFileXxHash(filePath string) (uint64, int64, error) {
	// 打开文件
	file, err := os.Open(filePath)
	if err != nil {
		return 0, 0, fmt.Errorf("error opening file: %s", err)
	}
	defer file.Close()
	// 获取文件大小 int64
	fileInfo, err := file.Stat()
	if err != nil {
		fmt.Println("获取文件信息失败：", err)
		return 0, 0, err
	}
	fileSize := fileInfo.Size()
	// 创建 xxhash 哈希对象
	hasher := xxhash.New()

	// 创建一个缓冲区，逐块读取文件
	buffer := make([]byte, 4096*512) // 调整缓冲区大小以适应需求

	// 逐块读取文件内容并更新哈希
	for {
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			return 0, 0, fmt.Errorf("error reading file: %s", err)
		}
		if n > 0 {
			hasher.Write(buffer[:n])
		}
		if err == io.EOF {
			break
		}
	}
	// 计算哈希值并返回
	return hasher.Sum64(), fileSize, nil
}

// func main1() {
// 	var str string
// 	//str = []byte("QWER12345")
// 	if len(os.Args) == 3 && os.Args[1] == "encode" {
// 		fmt.Println("请输入key：key为16，24，32位字符")
// 		fmt.Scanln(&PwdKey)
// 		//fmt.Println("请输入需要加密的密码：")
// 		//fmt.Scanln(&str)
// 		str = os.Args[2]
// 		pwd, _ := EnPwdCode([]byte(str))
// 		fmt.Println("加密的密码：", string(pwd))
// 	}
// 	if len(os.Args) == 3 && os.Args[1] == "decode" {
// 		fmt.Println("请输入key：key为16，24，32位字符")
// 		fmt.Scanln(&PwdKey)
// 		//fmt.Println("请输入需要解密的密码：")
// 		//fmt.Scanln(&str)
// 		str = os.Args[2]
// 		bytes, _ := DePwdCode(str)
// 		fmt.Println("解密的密码：", string(bytes))
// 	}
// 	if len(os.Args) == 4 && os.Args[1] == "encode" {
// 		str = os.Args[2]
// 		pwd, _ := EnPwdCode([]byte(str))
// 		fmt.Println("加密的密码：", string(pwd))
// 	}
// 	if len(os.Args) == 4 && os.Args[1] == "decode" {
// 		str = os.Args[2]
// 		bytes, _ := DePwdCode(str)
// 		fmt.Println("解密的密码：", string(bytes))
// 	}

// }
