package main

import (
	_ "embed"
	"fmt"
	"log"

	"github.com/chengcong1/forky/pkg/offauth"
)

//go:embed public_ed25519.key
var publicKey []byte

func main() {
	err := offauth.GenerateRequestFileType1("req")
	if err != nil {
		panic(err)
	}
	公钥验证1()
}
func 公钥验证1() {
	publicKey, err := offauth.LoadPublicKeyFromBytes(publicKey)
	if err != nil {
		fmt.Println(err)
	}
	// 从文件中读取并解析为offauth.License
	license, err := offauth.LoadLicenseFromFileType1("license")
	if err != nil {
		log.Println(err)
	}
	// 验证数据
	pass, err := offauth.VerifyLicenseType1(license, publicKey)
	// pass, err := offauth.VerifyOffAuth(machinecode, publicKey)
	if err != nil {
		log.Println(err)
	}
	if !pass {
		log.Println("验证失败")
	} else {
		log.Println("验证成功")
	}

}
