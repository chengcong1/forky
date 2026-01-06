package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/chengcong1/forky/pkg/offauth"
)

var help = `
Usage: offauth [genkey|sign|verify]
`

func main() {
	// addCmd := flag.NewFlagSet("genkey", flag.ExitOnError)
	signCmd := flag.NewFlagSet("sign", flag.ExitOnError)
	privateKey := signCmd.String("keypath", "private_ed25519.key", "private key path")
	validDays := signCmd.Int("days", 0, "validity period (days)")
	customer := signCmd.String("customer", "", "customer name")
	reqFile := signCmd.String("reqfile", "", "request license file")

	verifyCmd := flag.NewFlagSet("verify", flag.ExitOnError)
	switch os.Args[1] {
	case "genkey":
		genkey()
	case "sign":
		signCmd.Parse(os.Args[2:])
		if verifyParam(*privateKey, *reqFile, *validDays, *customer) {
			sign(*privateKey, *reqFile, *customer, *validDays)
			log.Println("Sign success")
		} else {
			log.Println("Sign failed")
		}
	case "verify":
		verifyCmd.Parse(os.Args[2:])
		verifySign()
	default:
		fmt.Println("Expected 'genkey' or 'sign' subcommands")
	}
}

func verifySign() {

}

func sign(privateKey, reqFile, customer string, validDays int) {
	k, err := offauth.LoadPrivateKeyFromFile(privateKey)
	if err != nil {
		log.Fatal(err)
	}
	reqInfo, err := offauth.LoadRequestFromFile(reqFile)
	if err != nil {
		log.Fatal(err)
	}
	license, err := offauth.GenerateLicense(reqInfo, customer, validDays, k)
	if err != nil {
		log.Fatal(err)
	}
	err = offauth.SaveLicenseToFile(license, "license")
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Generate license success")
}

// 验证参数
func verifyParam(privateKey string, reqFile string, validDays int, customer string) bool {
	var ok = true
	if validDays <= 0 {
		log.Printf("param error, validDays: %d\n", validDays)
		ok = false
	}
	if customer == "" {
		log.Printf("param error, customer: %s\n", customer)
		ok = false
	}
	if _, err := os.Stat(reqFile); os.IsNotExist(err) {
		log.Printf("param error, file not exits reqfile: %s", reqFile)
		ok = false
	}
	if _, err := os.Stat(privateKey); os.IsNotExist(err) {
		log.Printf("param error, file not exits privateKey: %s", privateKey)
		ok = false
	}
	if !ok {
		return false
	}
	return true
}

func genkey() {
	publicKeyName := "public_ed25519.pem"
	privateKeyName := "private_ed25519.pem"
	if _, err := os.Stat(publicKeyName); err == nil {
		log.Fatal("warn: public key already exists. If you need to generate a new file, please delete the existing file")
	}
	if _, err := os.Stat(privateKeyName); err == nil {
		log.Fatal("warn: private key already exists. If you need to generate a new file, please delete the existing file")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	err = offauth.SavePrivateKeyToFile(privateKey, privateKeyName)
	if err != nil {
		log.Fatal(err)
	}
	err = offauth.SavePublicKeyToFile(publicKey, publicKeyName)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Generate key success; public path: %s; private path: %s", publicKeyName, privateKeyName)
}
