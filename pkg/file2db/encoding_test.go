package file2db

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

const sampleText = "姓名,年龄\n张三,30\n"

// TestDecodeReader 覆盖「BOM -> UTF-8 校验 -> 回退 GB18030」这条编码探测链。
func TestDecodeReader(t *testing.T) {
	dir := t.TempDir()

	utf16le, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().String(sampleText)
	if err != nil {
		t.Fatal(err)
	}
	utf16be, err := unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewEncoder().String(sampleText)
	if err != nil {
		t.Fatal(err)
	}
	gbk, err := simplifiedchinese.GB18030.NewEncoder().String(sampleText)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		bytes    []byte
		want     string // 传进去的编码名
		wantName string // 识别出来的编码名
	}{
		{"utf8", []byte(sampleText), EncodingAuto, EncodingUTF8},
		{"utf8-bom", append([]byte{0xEF, 0xBB, 0xBF}, sampleText...), EncodingAuto, EncodingUTF8},
		{"gbk", []byte(gbk), EncodingAuto, EncodingGB18030},
		{"gbk-指定编码", []byte(gbk), EncodingGBK, EncodingGB18030},
		{"utf16le", []byte(utf16le), EncodingAuto, EncodingUTF16LE},
		{"utf16be", []byte(utf16be), EncodingAuto, EncodingUTF16BE},
		{"utf16le-指定", []byte(utf16le), "utf-16", EncodingUTF16LE},
		{"gb2312-别名", []byte(gbk), "gb2312", EncodingGB18030},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(dir, c.name+".csv")
			if err := os.WriteFile(path, c.bytes, 0o644); err != nil {
				t.Fatal(err)
			}
			got, gotName, err := decodeFile(t, path, c.want)
			if err != nil {
				t.Fatalf("decodeReader 失败: %v", err)
			}
			if gotName != c.wantName {
				t.Errorf("识别到的编码 = %q, want %q", gotName, c.wantName)
			}
			// 解码后必须是干净的 UTF-8，且 BOM 已被吃掉
			if strings.ContainsRune(got, '\ufeff') {
				t.Errorf("解码结果里还有 BOM: %q", got)
			}
			if got != sampleText {
				t.Errorf("解码结果 = %q, want %q", got, sampleText)
			}
		})
	}
}

func TestDecodeReaderEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.csv")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, name, err := decodeFile(t, path, EncodingAuto)
	if err != nil {
		t.Fatalf("空文件不应报错: %v", err)
	}
	if got != "" {
		t.Errorf("空文件解码结果应为空，得到 %q", got)
	}
	if name != EncodingAuto {
		t.Errorf("空文件编码名 = %q, want %q", name, EncodingAuto)
	}
}

func TestDecodeReaderUnsupportedEncoding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.csv")
	if err := os.WriteFile(path, []byte(sampleText), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := decodeFile(t, path, "big5"); err == nil {
		t.Fatal("不支持的编码应当报错")
	} else if !strings.Contains(err.Error(), "不支持的编码") {
		t.Errorf("错误信息应说明编码不支持，得到: %v", err)
	}
}

func TestSniffEncoding(t *testing.T) {
	gbk, err := simplifiedchinese.GB18030.NewEncoder().String(sampleText)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		bytes []byte
		want  string
	}{
		{"utf8-bom", []byte{0xEF, 0xBB, 0xBF, 'a'}, EncodingUTF8},
		{"utf16le-bom", []byte{0xFF, 0xFE, 'a', 0}, EncodingUTF16LE},
		{"utf16be-bom", []byte{0xFE, 0xFF, 0, 'a'}, EncodingUTF16BE},
		{"纯-ascii", []byte("name,age\n"), EncodingUTF8},
		{"utf8-中文", []byte(sampleText), EncodingUTF8},
		{"gbk-中文", []byte(gbk), EncodingGB18030},
	}
	for _, c := range cases {
		if got := sniffEncoding(c.bytes); got != c.want {
			t.Errorf("sniffEncoding(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLooksUTF8(t *testing.T) {
	truncated := []byte("中文abc中")
	truncated = truncated[:len(truncated)-1] // 末尾被 Peek 截断到半个字符
	cases := []struct {
		name  string
		bytes []byte
		want  bool
	}{
		{"完整中文", []byte("中文"), true},
		{"末尾被截断的多字节", truncated, true},
		{"GBK 双字节", []byte{0xD0, 0xD5, 0xC3, 0xFB, 0x2C}, false},
		{"空", nil, true},
	}
	for _, c := range cases {
		if got := looksUTF8(c.bytes); got != c.want {
			t.Errorf("looksUTF8(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestStripBOMOnlyWhenPresent(t *testing.T) {
	// 没有 BOM 时不能吃掉首字节
	br := bufio.NewReader(strings.NewReader("abc"))
	stripBOM(br, []byte("abc"), EncodingUTF8)
	all, _ := io.ReadAll(br)
	if string(all) != "abc" {
		t.Errorf("无 BOM 时读到的内容 = %q, want abc", string(all))
	}

	// 有 BOM 时必须只丢掉这 3 个字节
	br = bufio.NewReader(bytes.NewReader([]byte{0xEF, 0xBB, 0xBF, 'a', 'b'}))
	head, _ := br.Peek(5)
	stripBOM(br, head, EncodingUTF8)
	all, _ = io.ReadAll(br)
	if string(all) != "ab" {
		t.Errorf("有 BOM 时读到的内容 = %q, want ab", string(all))
	}
}

func TestHexPreview(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.bin")
	if err := os.WriteFile(path, []byte{0x41, 0x42, 0x43}, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := HexPreview(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != "4142" {
		t.Errorf("HexPreview = %q, want 4142", got)
	}
	if _, err := HexPreview(filepath.Join(t.TempDir(), "nope"), 4); err == nil {
		t.Error("文件不存在时 HexPreview 应报错")
	}
}

func TestEncodeWriter(t *testing.T) {
	t.Run("utf8-bom", func(t *testing.T) {
		var buf bytes.Buffer
		w, flush, err := encodeWriter(&buf, EncodingUTF8, true)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("abc")); err != nil {
			t.Fatal(err)
		}
		if flush != nil {
			if err := flush(); err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.HasPrefix(buf.Bytes(), []byte{0xEF, 0xBB, 0xBF}) {
			t.Errorf("UTF-8 + BOM 应以 EF BB BF 开头，得到 % X", buf.Bytes())
		}
	})

	t.Run("gbk", func(t *testing.T) {
		var buf bytes.Buffer
		w, flush, err := encodeWriter(&buf, EncodingGBK, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, "张三"); err != nil {
			t.Fatal(err)
		}
		if err := flush(); err != nil {
			t.Fatal(err)
		}
		decoded, err := simplifiedchinese.GB18030.NewDecoder().String(buf.String())
		if err != nil {
			t.Fatal(err)
		}
		if decoded != "张三" {
			t.Errorf("GBK 往返 = %q, want 张三", decoded)
		}
		if buf.Len() == len("张三") {
			t.Error("GBK 编码后不应该是 UTF-8 的字节数")
		}
	})

	t.Run("utf16le", func(t *testing.T) {
		var buf bytes.Buffer
		w, flush, err := encodeWriter(&buf, EncodingUTF16LE, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, "abc"); err != nil {
			t.Fatal(err)
		}
		if err := flush(); err != nil {
			t.Fatal(err)
		}
		decoded, err := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder().String(buf.String())
		if err != nil {
			t.Fatal(err)
		}
		if decoded != "abc" {
			t.Errorf("UTF-16LE 往返 = %q, want abc", decoded)
		}
	})

	t.Run("不支持的编码", func(t *testing.T) {
		if _, _, err := encodeWriter(io.Discard, "big5", false); err == nil {
			t.Fatal("不支持的导出编码应报错")
		}
	})
}

// decodeFile 用 decodeReader 读整个文件，返回解码后的文本与实际识别到的编码名。
func decodeFile(t *testing.T, path, want string) (string, string, error) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	r, name, err := decodeReader(bufio.NewReader(f), want)
	if err != nil {
		return "", "", err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return "", "", err
	}
	return string(data), name, nil
}
