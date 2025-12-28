package offauth

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	machinecode "github.com/chengcong1/forky/pkg/machine-code"
)

// type MachineCode struct {
// 	MachineId  string `json:"machine_id"`
// 	DiskSerial string `json:"disk_serial"`
// 	MacAddress string `json:"mac_address"`
// 	CpuId      string `json:"cpu_id"`
// }

type OffAuth struct {
	MachineFingerprint machinecode.MachineCode `json:"machine_fingerprint"` // 机器指纹
	IssueDate          string                  `json:"issue_date"`          // 签发日期
	ExpireDate         string                  `json:"expire_date"`         // 截至日期
	Customer           string                  `json:"customer"`            // 客户
	Signature          string                  `json:"signature"`           // 签名
}

type License struct {
	MachineFingerprint string `json:"machine_fingerprint"` // 机器指纹
	IssueDate          string `json:"issue_date"`          // 签发日期
	ExpireDate         string `json:"expire_date"`         // 截至日期
	Customer           string `json:"customer,omitempty"`  // 客户
	Signature          string `json:"signature,omitempty"` // 签名
}

func GenerateLicense(machineFingerprint, customer string, validDays int, privateKey ed25519.PrivateKey) (*License, error) {
	issueDate := time.Now()
	expireDate := issueDate.AddDate(0, 0, validDays)

	license := &License{
		MachineFingerprint: machineFingerprint,
		IssueDate:          issueDate.Format("2006-01-02"),
		ExpireDate:         expireDate.Format("2006-01-02"),
		Customer:           customer,
	}

	// 计算签名
	// data, err := json.Marshal(license)
	// if err != nil {
	// 	return nil, err
	// }
	// 注意：实际签名时，我们通常只对授权内容进行签名，不包括签名字段本身。所以我们在计算签名时，先置空Signature字段。
	license.Signature = ""
	data, err := json.Marshal(license)
	if err != nil {
		return nil, err
	}
	signature := ed25519.Sign(privateKey, data)
	license.Signature = base64.StdEncoding.EncodeToString(signature)
	return license, nil
}

func SaveLicenseToFile(license *License, filename string) error {
	data, err := json.MarshalIndent(license, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}
func GenerateOffAuth(machineFingerprint *machinecode.MachineCode, customer string, validDays int, privateKey ed25519.PrivateKey) (*OffAuth, error) {
	issueDate := time.Now()
	expireDate := issueDate.AddDate(0, 0, validDays)

	offAuth := &OffAuth{
		MachineFingerprint: *machineFingerprint,
		IssueDate:          issueDate.Format("2006-01-02"),
		ExpireDate:         expireDate.Format("2006-01-02"),
		Customer:           customer,
	}
	// fmt.Println("offAuth", offAuth)
	// 计算签名
	// data, err := json.Marshal(license)
	// if err != nil {
	// 	return nil, err
	// }
	// 注意：实际签名时，我们通常只对授权内容进行签名，不包括签名字段本身。所以我们在计算签名时，先置空Signature字段。
	offAuth.Signature = ""
	data, err := json.Marshal(offAuth)
	if err != nil {
		return nil, err
	}
	// fmt.Println("data", string(data))
	signature := ed25519.Sign(privateKey, data)
	offAuth.Signature = base64.StdEncoding.EncodeToString(signature)
	return offAuth, nil
}
func SaveOffAuthToFile(offAuth *OffAuth, filename string) error {
	data, err := json.MarshalIndent(offAuth, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

// SavePrivateKeyToFile 将ed25519私钥保存到文件（PEM格式）
func SavePrivateKeyToFile(privateKey ed25519.PrivateKey, filePath string) error {
	// 创建目录（如果不存在）
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	// 将私钥序列化为PKCS8格式
	keyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("序列化私钥失败: %w", err)
	}

	// 封装为PEM格式
	pemBlock := &pem.Block{
		Type:  "PRIVATE KEY", // PKCS8格式的私钥类型固定为PRIVATE KEY
		Bytes: keyBytes,
	}

	// 写入文件（设置仅当前用户可读写的权限）
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	if err := pem.Encode(file, pemBlock); err != nil {
		return fmt.Errorf("编码PEM失败: %w", err)
	}
	// fmt.Printf("the private key path: %s\n", filePath)
	return nil
}

// SavePublicKeyToFile 将ed25519公钥保存到文件（PEM格式）
func SavePublicKeyToFile(publicKey ed25519.PublicKey, filePath string) error {
	// 创建目录（如果不存在）
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	// 将公钥序列化为PKIX格式
	keyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return fmt.Errorf("序列化公钥失败: %w", err)
	}

	// 封装为PEM格式
	pemBlock := &pem.Block{
		Type:  "PUBLIC KEY", // 公钥类型固定为PUBLIC KEY
		Bytes: keyBytes,
	}

	// 写入文件
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	if err := pem.Encode(file, pemBlock); err != nil {
		return fmt.Errorf("编码PEM失败: %w", err)
	}

	// fmt.Printf("the public key path: %s\n", filePath)
	return nil
}

// LoadPrivateKeyFromFile 从文件读取ed25519私钥
func LoadPrivateKeyFromFile(filePath string) (ed25519.PrivateKey, error) {
	// 读取文件内容
	keyData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	// 解码PEM块
	block, _ := pem.Decode(keyData)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, errors.New("无效的私钥PEM格式")
	}

	// 解析PKCS8格式的私钥
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析私钥失败: %w", err)
	}

	// 类型断言为ed25519私钥
	privateKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("密钥不是ed25519类型")
	}

	return privateKey, nil
}

// LoadPublicKeyFromFile 从文件读取ed25519公钥
func LoadPublicKeyFromFile(filePath string) (ed25519.PublicKey, error) {
	// 读取文件内容
	keyData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	// 解码PEM块
	block, _ := pem.Decode(keyData)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, errors.New("无效的公钥PEM格式")
	}

	// 解析PKIX格式的公钥
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析公钥失败: %w", err)
	}

	// 类型断言为ed25519公钥
	publicKey, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("密钥不是ed25519类型")
	}

	return publicKey, nil
}

func LoadRequestFromFile(filename string) (string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	dataStr := string(data)
	machineCodeString, err := base64.StdEncoding.DecodeString(dataStr)
	var machineCode machinecode.MachineCode
	err = json.Unmarshal(machineCodeString, &machineCode)
	if err != nil {
		return "", err
	}
	var validFields int
	if machineCode.CpuId == "" {
		validFields++
	}
	if machineCode.DiskSerial == "" {
		validFields++
	}
	if machineCode.MacAddress == "" {
		validFields++
	}
	if machineCode.MachineId == "" {
		validFields++
	}
	if validFields > 1 { // 至少需要1个字段不为空
		return "", fmt.Errorf("Insufficient machine code information")
	}
	return dataStr, nil
}
