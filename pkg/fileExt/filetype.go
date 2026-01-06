package fileExt

import (
	"fmt"
	"os"

	"github.com/h2non/filetype"
)

/*
	文件内包含的函数以及简短的说明

1、ReadFileExt(filePath string) (Extension string)   获取不到时返回 "",自定义类型需在1、2、3新增

*/

// 注册一个新的文件类型1、2、3
func init() {
	// 3、在函数中注册
	filetype.AddMatcher(lz4Type, lz4Matcher)
	filetype.AddMatcher(lzcxType, lzcxMatcher)
}

// 1、定义类型名
var lz4Type = filetype.NewType("lz4", "application/lz4")
var lzcxType = filetype.NewType("lzcx", "application/lzcx")

// 2、填写类型的魔数
func lz4Matcher(buf []byte) bool {
	return len(buf) > 1 && buf[0] == 0x04 && buf[1] == 0x22 && buf[2] == 0x4D && buf[3] == 0x18
}
func lzcxMatcher(buf []byte) bool {
	return len(buf) > 1 && buf[1] == 0x6C && buf[2] == 0x7A && buf[3] == 0x63 && buf[4] == 0x78
}
func ReadFileExt(filePath string) (Extension string) {
	// 检查是否支持新的文件拓展名
	// if filetype.IsSupported("lz4") {
	// 	fmt.Println("New supported type: lz4")
	// }
	// 检查是否支持新的文件MIME
	// if filetype.IsMIMESupported("application/lz4") {
	// 	fmt.Println("New supported MIME type: application/lz4")
	// }
	// 打开一个文件
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Println("打开文件获取文件拓展名时遇到错误: ", err)
		return
	}
	defer file.Close()
	// 获取前261个字符
	head := make([]byte, 261)
	file.Read(head)
	kind, _ := filetype.Match(head)

	if kind == filetype.Unknown {
		// fmt.Println("Unknown file type")
		return ""
	} else {
		// fmt.Println("File type matched:", kind.Extension)
		return kind.Extension
	}

}
