package filecrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/chengcong1/forky/pkg/compress"
	"github.com/chengcong1/forky/pkg/crypto"
)

// 判断路径是否存在，返回值1，存在；返回值2，是否是目录；返回值3，错误信息
func inPutExist(inputFile string) (bool, bool, error) {
	info, err := os.Stat(inputFile)
	if os.IsNotExist(err) {
		return false, false, fmt.Errorf("路径不存在: %s", inputFile)
	}
	if err != nil {
		return false, false, fmt.Errorf("检查路径时出错: %s，%s", inputFile, err)
	}
	if info.IsDir() {
		// level = Level5
		return true, true, nil
	} else if info.Mode().IsRegular() {
		return true, false, nil
	} else {
		return false, false, fmt.Errorf("未知的路径类型")
	}
}

func GenerateEncryptFile(header CustomHeader, fileOtherInfo FileOtherInfo, key string) error {
	// 打开文件
	infile, err := os.Open(fileOtherInfo.InputFile)
	if err != nil {
		return err
	}
	defer infile.Close()
	// 创建输出文件
	outFile, err := os.Create(fileOtherInfo.OutputFile)
	if err != nil {
		return err
	}
	defer outFile.Close()
	// 写入文件头
	err = binary.Write(outFile, binary.LittleEndian, header)
	if err != nil {
		// fmt.Println("写入header遇到错误：", err)
		return err
	}
	if fileOtherInfo.EncryFileName != "" {
		// 写入文件名
		err = binary.Write(outFile, binary.LittleEndian, []byte(fileOtherInfo.EncryFileName))
		if err != nil {
			return fmt.Errorf("写入fileName遇到错误：%s", err)
		}
	}
	// 创建AES加密块
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return err
	}
	// 创建AES加密流
	// 对IV的2个元素进行调换位置，增加复杂性
	crypto.SliceItemSwap(header.Iv[:], 2, 5)
	stream := cipher.NewCTR(block, header.Iv[:])
	// 加密并写入输出文件
	buffer := make([]byte, defaultBufferSize) // 缓冲区
	for {
		n, err := infile.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		// 加密数据块
		stream.XORKeyStream(buffer[:n], buffer[:n])
		// 将加密后的数据写入输出文件
		_, err = outFile.Write(buffer[:n])
		if err != nil {
			return err
		}
	}
	// infile.Close()
	// outFile.Close()
	return nil
}

func GenerateEncryptOutFileUseLevel(inputFile string, level int8, key string) (string, error) {
	// 判断路径是否存在
	_, isDir, err := inPutExist(inputFile)
	if err != nil {
		return "", err
	}
	if isDir {
		level = Level5
		// inputFile 是否为 / 结尾
		zipFile := removeTrailingBackslash(inputFile) + ".zip"
		if err := compress.Zip(zipFile, inputFile); err != nil {
			return "", err
		}
		inputFile = zipFile
		defer func() {
			// 删除目录压缩文件
			if err := os.Remove(zipFile); err != nil {
				panic(err)
			}
		}()
	}

	k, err := handleEncryptKey(key, level)
	if err != nil {
		return "", err
	}
	header, fileOtherInfo, err := GenerateHeader(inputFile, level, k)
	if err != nil {
		return "", fmt.Errorf("GenerateHeader:%s", err)
	}
	var outFileName string
	switch level {
	case Level2, Level_1, Level5:
		// 生成加密文件
		err = GenerateEncryptFile(header, fileOtherInfo, k)
		if err != nil {
			return "", fmt.Errorf("GenerateEncryptFile:%s", err)
		}

	}

	if level >= 0 {
		outFileName = fmt.Sprintf("%s.lzc%d", fileOtherInfo.InputFile, level)
	} else {
		outFileName = fmt.Sprintf("%s.lzcx", fileOtherInfo.InputFile)
	}

	if os.Rename(fileOtherInfo.OutputFile, outFileName) != nil {
		return fileOtherInfo.OutputFile, fmt.Errorf("os.Rename:%s", err)
	}
	return outFileName, nil
}
