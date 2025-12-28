package base85

import (
	"encoding/ascii85"
	"fmt"
	"os"
)

// Base85Encode 标准Ascii85编码（Adobe格式）
func Base85Encode(data []byte) string {
	// 计算编码后长度：每4字节输入对应5字节输出，不足补位
	encodedLen := ascii85.MaxEncodedLen(len(data))
	buf := make([]byte, encodedLen)

	// 执行编码
	n := ascii85.Encode(buf, data)

	// 返回有效编码结果（去除补位后的空字节）
	return string(buf[:n])
}

// Base85Decode 标准Ascii85解码
func Base85Decode(encoded string) ([]byte, error) {
	// 	Ascii85 编码是 5 个字符对应最多 4 个字节 的数据：
	// • 每 5 个 ASCII85 字符解码为最多 4 个字节
	// • 特殊字符 'z' 代表 4 个零字节，仍符合这个比例
	// • 不足 5 个字符的末尾块会解码为更少的字节（但最多仍为 4 个字节）
	//                                            ---- 通义灵码

	// 计算解码后最大长度
	maxDecodedLen := (len(encoded)+4)/5*4 + 10
	buf := make([]byte, maxDecodedLen)

	// 执行解码
	n, _, err := ascii85.Decode(buf, []byte(encoded), true)
	if err != nil {
		return nil, fmt.Errorf("解码失败: %w", err)
	}

	// 返回有效解码结果
	return buf[:n], nil
}

// EncodeFileToBase85 将文件编码为Base85字符串并保存
func EncodeFileToBase85(inputFile, outputFile string) error {
	// 读取文件二进制数据
	data, err := os.ReadFile(inputFile)
	if err != nil {
		return err
	}

	// 编码
	buf := make([]byte, ascii85.MaxEncodedLen(len(data)))
	n := ascii85.Encode(buf, data)

	// 保存编码结果
	return os.WriteFile(outputFile, buf[:n], 0644)
}

// DecodeBase85ToFile 将Base85字符串解码为文件
func DecodeBase85ToFile(inputFile, outputFile string) error {
	// 读取Base85编码内容
	encodedData, err := os.ReadFile(inputFile)
	if err != nil {
		return err
	}

	// 解码
	maxDecodedLen := (len(encodedData) + 4) / 5 * 4
	buf := make([]byte, maxDecodedLen)
	n, _, err := ascii85.Decode(buf, encodedData, true)
	if err != nil {
		return err
	}

	// 保存解码后的文件
	return os.WriteFile(outputFile, buf[:n], 0644)
}
