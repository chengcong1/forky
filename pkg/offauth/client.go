package offauth

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	machinecode "github.com/chengcong1/forky/pkg/machine-code"
)

func warpErr(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	s := ""
	for _, err := range errs {
		if err != nil {
			s = s + ";" + err.Error()
		}
	}
	return errors.New(s)
}

func GenerateRequestFile() error {
	machineFingerprint, err := GetMachineFingerprintBase64()
	if err != nil {
		return err
	}
	return os.WriteFile("req", []byte(machineFingerprint), 0644)
}
func GetMachineFingerprint() (string, error) {
	machineCode, errs := machinecode.GetMachineCode()
	if len(errs) > 1 {
		return "", warpErr(errs)
	}
	machineFingerprint := machineCode.CpuId + "|" + machineCode.DiskSerial + "|" + machineCode.MacAddress + "|" + machineCode.MachineId
	// 组合并哈希
	hash := sha256.Sum256([]byte(machineFingerprint))
	return hex.EncodeToString(hash[:]), nil
}
func GetMachineFingerprintBase64() (string, error) {
	machineCode, errs := machinecode.GetMachineCode()
	if len(errs) > 1 {
		return "", warpErr(errs)
	}
	j, err := json.Marshal(machineCode)
	if err != nil {
		return "", err
	}
	// return base85.Base85Encode(j), nil
	return base64.StdEncoding.EncodeToString(j), nil
}
func GetMachineFingerprintExt() (*machinecode.MachineCode, error) {
	machineCode, errs := machinecode.GetMachineCode()
	if len(errs) > 1 {
		return &machinecode.MachineCode{}, warpErr(errs)
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
	if validFields >= 3 { // 至少需要3个字段不为空
		return &machinecode.MachineCode{}, warpErr(errs)
	}
	// machineCode := MachineCode{
	// 	MachineId:  machineCode.MachineId,
	// 	CpuId:      machineCode.CpuId,
	// 	DiskSerial: machineCode.DiskSerial,
	// 	MacAddress: machineCode.MacAddress,
	// }
	// fmt.Println("machineCode", machineCode)
	return &machineCode, nil
}
func VerifyLicense(license *License, publicKey ed25519.PublicKey) (bool, error) {
	// 保存签名，然后置空以便计算哈希
	signature := license.Signature
	license.Signature = ""

	data, err := json.Marshal(license)
	if err != nil {
		return false, err
	}

	// 恢复签名
	license.Signature = signature

	// 解码签名
	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false, err
	}

	// 验证签名
	verifyResult := ed25519.Verify(publicKey, data, sigBytes)
	if !verifyResult {
		return false, fmt.Errorf("signature verification failed")
	}
	// 验证机器指纹
	currentFingerprint, err := GetMachineFingerprint()
	if err != nil {
		return false, err
	}
	if currentFingerprint != license.MachineFingerprint {
		return false, fmt.Errorf("machine fingerprint mismatch")
	}

	// 验证有效期
	now := time.Now()
	expireDate, err := time.Parse("2006-01-02", license.ExpireDate)
	if err != nil {
		return false, err
	}
	if now.After(expireDate) {
		return false, fmt.Errorf("license expired")
	}

	return true, nil
}
func LoadLicenseFromFile(filename string) (*License, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var license License
	err = json.Unmarshal(data, &license)
	if err != nil {
		return nil, err
	}
	return &license, nil
}

func VerifyOffAuth(offAuth *OffAuth, publicKey ed25519.PublicKey) (bool, error) {
	// 保存签名，然后置空以便计算哈希
	signature := offAuth.Signature
	offAuth.Signature = ""

	data, err := json.Marshal(offAuth)
	if err != nil {
		return false, err
	}
	// fmt.Println("data", string(data))
	// 恢复签名
	offAuth.Signature = signature
	// 解码签名
	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false, err
	}
	// 验证签名
	verifyResult := ed25519.Verify(publicKey, data, sigBytes)
	if !verifyResult {
		return false, fmt.Errorf("signature verification failed")
	}
	// 验证机器指纹
	currentFingerprint, err := GetMachineFingerprintExt()
	if err != nil {
		return false, err
	}
	var validFields int
	if currentFingerprint.CpuId != offAuth.MachineFingerprint.CpuId || currentFingerprint.CpuId == "" {
		validFields++
	}
	if currentFingerprint.MachineId != offAuth.MachineFingerprint.MachineId || currentFingerprint.MachineId == "" {
		validFields++
	}
	if currentFingerprint.DiskSerial != offAuth.MachineFingerprint.DiskSerial || currentFingerprint.DiskSerial == "" {
		validFields++
	}
	if currentFingerprint.MacAddress != offAuth.MachineFingerprint.MacAddress || currentFingerprint.MacAddress == "" {
		validFields++
	}
	if validFields > 3 { // 至少需要验证3个字段不一致
		return false, fmt.Errorf("machine fingerprint mismatch")
	}
	// 验证有效期
	now := time.Now()
	expireDate, err := time.Parse("2006-01-02", offAuth.ExpireDate)
	if err != nil {
		return false, err
	}
	if now.After(expireDate) {
		return false, fmt.Errorf("license expired")
	}

	return true, nil

}
func LoadOffAuthFromFile(filename string) (*OffAuth, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var offAuth OffAuth
	err = json.Unmarshal(data, &offAuth)
	if err != nil {
		return nil, err
	}
	return &offAuth, nil
}
