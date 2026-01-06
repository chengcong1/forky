package compress

import (
	"io"
	"os"

	"github.com/pierrec/lz4"
)

/*
1、CompressFileLz4(inputFile, outputFile string) error
2、
*/

// 使用lz4进行压缩
func CompressFileLz4(inputFile, outputFile string) error {
	// 打开源文件
	file, err := os.Open(inputFile)
	if err != nil {
		return err
	}
	defer file.Close()
	// 创建目标文件用于写入压缩后的数据
	outFile, err := os.Create(outputFile)
	if err != nil {
		panic(err)
	}
	defer outFile.Close()

	// 创建一个LZ4写入器
	writer := lz4.NewWriter(outFile)
	defer writer.Close()

	// 创建一个缓冲区用于从源文件读取数据
	buf := make([]byte, 4096) // 4KB的缓冲区，可以根据需要调整

	// 读取源文件并写入LZ4写入器
	for {
		n, err := file.Read(buf)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
			break
		}
		if _, err := writer.Write(buf[:n]); err != nil {
			return err
		}
	}
	// 确保所有数据都被写入
	if err := writer.Flush(); err != nil {
		return err
	}
	return nil
}
