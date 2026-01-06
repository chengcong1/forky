package fileExt

import (
	"os"

	"github.com/gabriel-vasile/mimetype"
)

func init() {
	lz4Detector := func(buf []byte, limit uint32) bool {
		return len(buf) > 1 && buf[0] == 0x04 && buf[1] == 0x22 && buf[2] == 0x4D && buf[3] == 0x18
	}
	lzcxDetector := func(buf []byte, limit uint32) bool {
		return len(buf) > 1 && buf[1] == 0x6C && buf[2] == 0x7A && buf[3] == 0x63 && buf[4] == 0x78
	}

	mimetype.Lookup("application/octet-stream").Extend(lz4Detector, "application/lz4", ".lz4")
	mimetype.Lookup("application/octet-stream").Extend(lzcxDetector, "application/lzcx", ".lzc")
}

func DetectMimeType(filename string) (string, error) {
	// 打开文件
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// 获取文件的 MIME 类型
	mime, err := mimetype.DetectReader(file)
	if err != nil {
		return "", err
	}
	// fmt.Println(mime.String())
	return mime.Extension(), nil
}

// func main() {
// 	filename := "outFile.bin"

// 	// 检测文件的 MIME 类型
// 	mime, err := DetectMimeType(filename)
// 	if err != nil {
// 		log.Fatalf("Failed to detect MIME type: %v", err)
// 	}

// 	fmt.Printf("MIME type of %s: %s\n", filename, mime)
// }
