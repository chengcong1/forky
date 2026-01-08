package main

// package main

// import (
// 	"crypto/ed25519"
// 	"crypto/rand"
// 	"crypto/x509"
// 	_ "embed"
// 	"encoding/base64"
// 	"encoding/hex"
// 	"encoding/json"
// 	"encoding/pem"
// 	"fmt"
// 	"log"
// 	"os"
// 	"time"

// 	"github.com/chengcong1/forky/pkg/crypto/base85"
// 	machinecode "github.com/chengcong1/forky/pkg/machine-code"
// 	"github.com/chengcong1/forky/pkg/offauth"
// )

// //go:embed public_ed25519.key
// var publicKey []byte

// //go:embed private_ed25519.key
// var privateKey []byte

// var k = byte(0xc)

// func main() {
// 	main1()
// 	// main2()
// 	公钥验证()
// 	私钥签发()
// 	// base851()
// 	// 公钥验证()
// 	// 私钥签发1()
// 	// 公钥验证1()
// }
// func 公钥验证1() {
// 	// 解码PEM块
// 	block, _ := pem.Decode(publicKey)
// 	if block == nil || block.Type != "PUBLIC KEY" {
// 		fmt.Println("无效的公钥PEM格式")
// 	}
// 	// 解析PKIX格式的公钥
// 	key, err := x509.ParsePKIXPublicKey(block.Bytes)
// 	if err != nil {
// 		fmt.Println("解析公钥失败: %w", err)
// 	}
// 	// 类型断言为ed25519公钥
// 	publicKey, ok := key.(ed25519.PublicKey)
// 	if !ok {
// 		fmt.Println("密钥不是ed25519类型")
// 	}
// 	// 从文件中读取并解析为offauth.License
// 	data, err := os.ReadFile("linense.dat")
// 	if err != nil {
// 		fmt.Println(err)
// 	}
// 	var license offauth.License
// 	err = json.Unmarshal(data, &license)
// 	if err != nil {
// 		fmt.Println(err)
// 	}
// 	// base85解码
// 	// machineFingerprint, err := base85.Base85Decode(license.MachineFingerprint)
// 	machineFingerprint, err := base64.StdEncoding.DecodeString(license.MachineFingerprint)
// 	if err != nil {
// 		log.Println(err)
// 	}
// 	// json解码 获取 machineCode
// 	var machineCode machinecode.MachineCode
// 	err = json.Unmarshal(machineFingerprint, &machineCode)
// 	if err != nil {
// 		log.Println(err)
// 	}
// 	// 验证数据
// 	// 保存签名，然后置空以便计算哈希
// 	signature := license.Signature
// 	license.Signature = ""

// 	data, err = json.Marshal(license)
// 	if err != nil {
// 		log.Println(err)
// 	}
// 	// 恢复签名
// 	license.Signature = signature
// 	log.Println(signature)
// 	// 解码签名 / 解码签名 base85
// 	sigBytes, err := base64.StdEncoding.DecodeString(signature)
// 	// sigBytes, err := base85.Base85Decode(signature)
// 	if err != nil {
// 		log.Println(err)
// 	}
// 	// sigBytes = crypto.ReverseBit(sigBytes)
// 	// 验证签名
// 	verifyResult := ed25519.Verify(publicKey, data, sigBytes)
// 	if !verifyResult {
// 		log.Println("signature verification failed")
// 	}
// 	// 验证机器指纹
// 	currentCode, errs := machinecode.GetMachineCode()
// 	if err != nil {
// 		log.Println(errs)
// 	}
// 	var validFields int
// 	if currentCode.CpuId != machineCode.CpuId || currentCode.CpuId == "" {
// 		validFields++
// 	}
// 	if currentCode.MachineId != machineCode.MachineId || currentCode.MachineId == "" {
// 		validFields++
// 	}
// 	if currentCode.DiskSerial != machineCode.DiskSerial || currentCode.DiskSerial == "" {
// 		validFields++
// 	}
// 	if currentCode.MacAddress != machineCode.MacAddress || currentCode.MacAddress == "" {
// 		validFields++
// 	}
// 	if validFields > 3 { // 至少需要验证3个字段不一致
// 		log.Println("machine fingerprint mismatch")
// 	}
// 	// 验证有效期
// 	now := time.Now()
// 	expireDate, err := time.Parse("2006-01-02", license.ExpireDate)
// 	if err != nil {
// 		log.Println(err)
// 	}
// 	if now.After(expireDate) {
// 		log.Println("license expired")
// 	}
// 	log.Println("验证成功")
// 	// pass, err := offauth.VerifyOffAuth(machinecode, publicKey)
// 	// if err != nil {
// 	// 	log.Println(err)
// 	// }
// 	// if !pass {
// 	// 	log.Println("验证失败")
// 	// 	return
// 	// }

// }
// func 私钥签发1() {
// 	// 解码PEM块
// 	block, _ := pem.Decode(privateKey)
// 	if block == nil || block.Type != "PRIVATE KEY" {
// 		fmt.Println("无效的私钥PEM格式")
// 	}

// 	// 解析PKCS8格式的私钥
// 	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
// 	if err != nil {
// 		fmt.Println("解析私钥失败: %w", err)
// 	}

// 	// 类型断言为ed25519私钥
// 	privateKey, ok := key.(ed25519.PrivateKey)
// 	if !ok {
// 		fmt.Println("密钥不是ed25519类型")
// 	}
// 	machineFingerprint, err := offauth.GetMachineFingerprintBase85()
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	// 构建签名
// 	issueDate := time.Now()
// 	expireDate := issueDate.AddDate(0, 0, 3)

// 	license := &offauth.License{
// 		MachineFingerprint: machineFingerprint,
// 		IssueDate:          issueDate.Format("2006-01-02"),
// 		ExpireDate:         expireDate.Format("2006-01-02"),
// 		Customer:           "appName",
// 	}
// 	// license, err := offauth.GenerateLicense(machineFingerprint, "appName", 3, privateKey)
// 	license.Signature = ""
// 	data, err := json.Marshal(license)
// 	if err != nil {
// 		log.Println(err)
// 	}
// 	signature := ed25519.Sign(privateKey, data)
// 	// license.Signature = base85.Base85Encode(signature)
// 	// license.Signature = base64.StdEncoding.EncodeToString(signature)
// 	license.Signature = base64.StdEncoding.EncodeToString(signature)
// 	err = offauth.SaveLicenseToFile(license, "linense.dat")
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// }

// func base851() {
// 	data := []byte("Hello, 世界!")
// 	fmt.Println(data)
// 	fmt.Println([]rune(string(data)))
// 	// 编码为Base85
// 	// encoded := base85.Encode(data)
// 	encoded := base85.Base85Encode(data)
// 	fmt.Println("Encoded:", string(encoded))

// 	// 解码回原始数据
// 	decoded, err := base85.Base85Decode(encoded)
// 	if err != nil {
// 		panic(err)
// 	}
// 	fmt.Println("Decoded:", string(decoded))
// }
// func 私钥签发() {
// 	// 解码PEM块
// 	block, _ := pem.Decode(privateKey)
// 	if block == nil || block.Type != "PRIVATE KEY" {
// 		fmt.Println("无效的私钥PEM格式")
// 	}

// 	// 解析PKCS8格式的私钥
// 	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
// 	if err != nil {
// 		fmt.Println("解析私钥失败: %w", err)
// 	}

// 	// 类型断言为ed25519私钥
// 	privateKey, ok := key.(ed25519.PrivateKey)
// 	if !ok {
// 		fmt.Println("密钥不是ed25519类型")
// 	}
// 	machineFingerprint, err := offauth.GetMachineFingerprintExt()
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	license, err := offauth.GenerateOffAuth(machineFingerprint, "appName", 3, privateKey)
// 	err = offauth.SaveOffAuthToFile(license, "linense.dat")
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// }
// func 公钥验证() {
// 	// 解码PEM块
// 	block, _ := pem.Decode(publicKey)
// 	if block == nil || block.Type != "PUBLIC KEY" {
// 		fmt.Println("无效的公钥PEM格式")
// 	}

// 	// 解析PKIX格式的公钥
// 	key, err := x509.ParsePKIXPublicKey(block.Bytes)
// 	if err != nil {
// 		fmt.Println("解析公钥失败: %w", err)
// 	}

// 	// 类型断言为ed25519公钥
// 	publicKey, ok := key.(ed25519.PublicKey)
// 	if !ok {
// 		fmt.Println("密钥不是ed25519类型")
// 	}
// 	license1, err := offauth.LoadOffAuthFromFile("linense.dat")
// 	if err != nil {
// 		log.Println(err)
// 	}
// 	pass, err := offauth.VerifyOffAuth(license1, publicKey)
// 	if err != nil {
// 		log.Println(err)
// 	}
// 	if !pass {
// 		log.Println("验证失败")
// 		return
// 	}
// 	log.Println("验证成功")
// }
// func main2() {
// 	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	fmt.Printf("公钥（十六进制）：%s\n", hex.EncodeToString(publicKey))
// 	fmt.Printf("私钥（十六进制）：%s\n\n", hex.EncodeToString(privateKey))
// 	machineFingerprint, err := offauth.GetMachineFingerprintExt()
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	fmt.Println("machineFingerprint", machineFingerprint)
// 	license, err := offauth.GenerateOffAuth(machineFingerprint, "appName", 3, privateKey)
// 	err = offauth.SaveOffAuthToFile(license, "linense.dat")
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	// time.Sleep(time.Minute * 1)
// 	license1, err := offauth.LoadOffAuthFromFile("linense.dat")
// 	pass, err := offauth.VerifyOffAuth(license1, publicKey)
// 	if err != nil {
// 		log.Println(err)
// 	}
// 	if !pass {
// 		log.Println("验证失败")
// 		return
// 	}
// 	log.Println("验证成功")
// }
// func main1() {
// 	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	fmt.Printf("公钥（十六进制）：%s\n", hex.EncodeToString(publicKey))
// 	fmt.Printf("私钥（十六进制）：%s\n\n", hex.EncodeToString(privateKey))
// 	machineFingerprint, err := offauth.GetMachineFingerprint()
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	fmt.Println("machineFingerprint", machineFingerprint)
// 	license, err := offauth.GenerateLicense(machineFingerprint, "appName", 3, privateKey)
// 	err = offauth.SaveLicenseToFile(license, "linense.dat")
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	// time.Sleep(time.Minute * 1)
// 	license1, err := offauth.LoadLicenseFromFile("linense.dat")
// 	pass, err := offauth.VerifyLicense(license1, publicKey)
// 	if !pass {
// 		log.Println("验证失败")
// 		return
// 	}
// 	log.Println("验证成功")
// }
