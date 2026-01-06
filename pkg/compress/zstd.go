package compress

import (
	"fmt"
	"os"

	"github.com/klauspost/compress/zstd"
)

/*
1、CompressFileZstd(inputFile, outputFile string, compressionLevel zstd.EncoderLevel) error
2、DecompressFileZstd(compressedFile, outputFile string) error

*/

// 使用zstd压缩算法进行压缩 传入3个参数1、需要压缩的文件 2、压缩后的文件 3、压缩等级
func CompressFileZstd(inputFile, outputFile string) error {
	// 打开输入文件
	infile, err := os.Open(inputFile)
	if err != nil {
		return err
	}
	defer infile.Close()
	// 创建输出文件
	outFile, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer outFile.Close()
	// 创建压缩器，使用指定压缩等级
	encoder, err := zstd.NewWriter(outFile)
	if err != nil {
		return err
	}
	defer encoder.Close()

	// 使用适当大小的缓冲区进行分块压缩
	buf := make([]byte, 32*1024) // 32KB 缓冲区
	for {
		// 从输入文件读取数据块
		n, err := infile.Read(buf)
		if err != nil && err.Error() != "EOF" {
			return err
		}
		if n == 0 {
			break
		}
		// 将数据块写入压缩器
		_, err = encoder.Write(buf[:n])
		if err != nil {
			return err
		}
	}
	// 确保所有数据都被写入并压缩
	if err := encoder.Flush(); err != nil {
		return err
	}
	return nil
}

// 使用zstd压缩算法进行解压缩 传入2个参数1、需要解压缩的文件 2、解压缩后的文件
func DecompressFileZstd(compressedFile, outputFile string) error {
	// 打开压缩文件
	infile, err := os.Open(compressedFile)
	if err != nil {
		return err
	}
	defer infile.Close()
	// 创建输出文件
	outFile, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer outFile.Close()
	// 创建解压器
	decoder, err := zstd.NewReader(infile)
	if err != nil {
		return err
	}
	defer decoder.Close() // 关闭解压器
	// 将解压数据写入输出文件
	_, err = outFile.ReadFrom(decoder)
	if err != nil {
		fmt.Println(err)
		return err
	}
	return nil
}

// func main() {
// 	inputFile := "largefile.dat"
// 	outputFile := "compressed.zst"
// 	compressionLevel := zstd.SpeedDefault // 使用默认压缩等级
// 	err := CompressFileZstd(inputFile, outputFile, compressionLevel)
// 	if err != nil {
// 		fmt.Println(err)
// 	}
// 	fmt.Printf("文件 %s 成功压缩到 %s\n", inputFile, outputFile)
// }
