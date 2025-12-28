package crypto

import (
	"encoding/base64"
)

func Base64DecodeString(content string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(content)
}
func Base64EncodeToString(content []byte) string {
	return base64.StdEncoding.EncodeToString(content)
}

// 异或操作（XOR）key:= 0x01
func xorBytes(bytes []byte, key byte) []byte {
	for i := range bytes {
		// bytes[i] = ^bytes[i] // 按位取反
		bytes[i] ^= key // 异或操作（XOR）XOR操作
	}
	return bytes
}

// func main() {
//     data := []byte("Hello, this is a test for encoding efficiency!")

//     // Base64 编码
//     encoded64 := base64.StdEncoding.EncodeToString(data)
//     fmt.Printf("Base64: %s (len: %d)\n", encoded64, len(encoded64))

//     // Base85 编码
//     buf := make([]byte, ascii85.MaxEncodedLen(len(data)))
//     n := ascii85.Encode(buf, data)
//     encoded85 := string(buf[:n])
//     fmt.Printf("Base85: %s (len: %d)\n", encoded85, n)
// }
