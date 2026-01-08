package filecrypto

import (
	"crypto/aes"
	"crypto/rand"
)

var magicNumber = [4]byte{108, 122, 99, 120}

const HeaderLength = int(1 + 4 + 2 + 8 + 8 + 64 + 16 + 8)

var suffix = ".lzcxtemp"

var bufferSize = 4096 * 512

// 添加更多头部信息 1+4+2+8+8+64+16+8=111个字节
type CustomHeader struct {
	Level       int8     // 版本号，占用1个字节
	MagicNumber [4]byte  // 魔术值，占用4个字节
	NameLength  uint16   // 加密后的文件名的长度，占用2个字节
	FileLength  int64    // 因为fileInfo.Size() 使用int64，占用8个字节
	FileXxhash  uint64   // 文件的xxhash 快速哈希算法用于校验文件是否一致，返回uint64，占用8个字节
	KeyString   [64]byte // 用来存放用户自定义经过系统加密后的key的值，或者存放key的sha256的值，占用64个字节
	Iv          [16]byte // 用于AES加密 存放iv，占用16个字节
	Other       int64    // 扩展使用，占用8个字节
}

// 定义工厂函数
func NewHeader() CustomHeader {
	return CustomHeader{
		// Level:       level,
		MagicNumber: magicNumber,
		Iv:          getIv(),
	}
}

// 得到随机的iv 用于 aes加密
func getIv() [16]byte {
	var ivv [16]byte
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		panic(err)
	}
	copy(ivv[:16], iv)
	return ivv
}
