package filecrypto

import (
	"crypto/aes"
	"crypto/rand"
	"strings"
)

var magicNumber = [4]byte{108, 122, 99, 120}

const HeaderLength = int(1 + 4 + 2 + 8 + 8 + 64 + 16 + 8)

var suffix = ".lzcxtemp"

var defaultExt = ".lzcx"

var defaultBufferSize = 4096 * 512

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

const (
	Level_1 = int8(-1) // lzcx   自定义Key  仅加密
	Level_2 = int8(-2) // lzcx   自定义Key  压缩+加密

	Level0 = int8(1) // lzc0   仅拷贝
	Level1 = int8(2) // lzc1   仅压缩
	Level2 = int8(3) // lzc2   仅加密
	Level3 = int8(4) // lzc3   压缩+加密

	Level5 = int8(5) // lzc5   压缩文件夹
)

// 其他信息，存放文件名和加密后的文件名
type FileOtherInfo struct {
	InputFile     string
	OutputFile    string
	FileName      string
	EncryFileName string
}

// 如果文件夹路径是以/结尾，去掉最后一个/，如果是\结尾，去掉最后一个\
func removeTrailingBackslash(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	if strings.HasSuffix(path, "/") {
		return path[:len(path)-1]
	}
	return path
}
func GenerateHeader() {

}
