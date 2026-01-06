package xlsx

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/xuri/excelize/v2"
)

/*
	文件内包含的函数以及简短的说明

1、XlsmToXlsx(srcPath string) // xlsm转Xlsx
2、CsvToXlsxAllStr(csvPath, xlsxPath string) // csv转Xlsx，全部按string写入xlsx
3、CsvToXlsx(csvPath, xlsxPath string) // csv转Xlsx，可以转为float64的按数字写入，其他按string写入
4、XlsxToCSV(filePath string) // xlsx转CSV,仅需要xlsx的路径，每个sheet生成一个csv文件，使用请注意是否满足需求


*/

// 1、xlsm转Xlsx
func XlsmToXlsx(srcPath string) {
	if filepath.Ext(srcPath) == ".xlsm" || filepath.Ext(srcPath) == ".XLSM" {

		f, err := excelize.OpenFile(srcPath)
		if err != nil {
			fmt.Println(err)
			return
		}
		defer func() {
			if err := f.Close(); err != nil {
				fmt.Println(err)
			}
		}()
		if err := f.SaveAs(srcPath + ".xlsx"); err != nil {
			fmt.Println(err)
		}
	} else {
		fmt.Println("xlsm转xlsx遇到错误：文件后缀名不对，文件后缀必须是`.xlsm`或者`.XLSM`")
		return
	}
}

// 2、csv转Xlsx，全部按string写入xlsx
func CsvToXlsxAllStr(csvPath, xlsxPath string) {
	csvFile, err := os.Open(csvPath)
	if err != nil {
		fmt.Println("读取文件失败：", err)
		return
	}
	defer csvFile.Close()
	reader := csv.NewReader(csvFile)

	f := excelize.NewFile()
	sheetName := "sheet1"
	row := 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Println("读取csv行错误：", err)
			break
		}
		cell, err := excelize.CoordinatesToCellName(1, row)
		if err != nil {
			fmt.Println("索引转单元格坐标错误：", err)
			break
		}

		if err := f.SetSheetRow(sheetName, cell, &record); err != nil {
			fmt.Println("按行写入错误：", err)
			return
		}
	}
	if err := f.SaveAs(xlsxPath); err != nil {
		fmt.Println("文件另存错误：", err)
	}
}

// 3、csv转Xlsx，可以转为float64的按数字写入，其他按string写入
func CsvToXlsx(csvPath, xlsxPath string) {
	csvFile, err := os.Open(csvPath)
	if err != nil {
		fmt.Println("读取文件失败：", err)
		return
	}
	defer csvFile.Close()
	reader := csv.NewReader(csvFile)

	f := excelize.NewFile()
	sheetName := "sheet1"
	row := 1

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Println("读取csv行错误：", err)
			break
		}
		cell, err := excelize.CoordinatesToCellName(1, row)
		if err != nil {
			fmt.Println("索引转单元格坐标错误：", err)
			break
		}

		// if err := f.SetSheetRow(sheetName, cell, &record); err != nil {
		// 	fmt.Println("按行写入错误：", err)
		// 	return
		// }
		if row == 1 {
			if err := f.SetSheetRow(sheetName, cell, &record); err != nil {
				fmt.Println("按行写入错误：", err)
				return
			}
			row++
			continue
		}
		numbers, err := converSlice(record)
		if err != nil {
			fmt.Println("转数字格式错误：", err)
			break
		}
		if err := f.SetSheetRow(sheetName, cell, &numbers); err != nil {
			fmt.Println("按行写入错误：", err)
			break
		}
		row++
	}

	// 根据指定路径保存文件
	if err := f.SaveAs(xlsxPath); err != nil {
		fmt.Println("文件另存错误：", err)
	}
}

// 由于抄袭的代码在CSV最后一列中不是数字会报转换语法错误退出程序，原因未知，无法解决，这里我增加了2行来规避这个问题
func converSlice(record []string) (numbers []interface{}, err error) {
	record = append(record, "1") // 1、在切片最后新增一个可以转为float64的"1"
	for _, arg := range record {
		var n float64
		if n, err = strconv.ParseFloat(arg, 64); err == nil {
			numbers = append(numbers, n)
			continue
		}
		numbers = append(numbers, arg)
	}
	numbers = numbers[:len(record)-1] // 2、在转换完后的切片中，去除掉最后面新增的"1"
	return
}

// 4、xlsx转CSV,仅需要xlsx的路径，每个sheet生成一个csv文件，使用请注意是否满足需求
func XlsxToCSV(filePath string) {
	// 打开XLSX文件
	xlsxFile, err := excelize.OpenFile(filePath)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() {
		if err := xlsxFile.Close(); err != nil {
			fmt.Println(err)
		}
	}()
	// 获取文件内所有的表（sheet）
	// 结果是一个[]string字符串数组，里面是各个表的名称
	sheets := xlsxFile.GetSheetList()
	fmt.Println("xlsx文件sheet数量：", len(sheets))

	for _, sheetName := range sheets {
		rows, err := xlsxFile.GetRows(sheetName)
		if err != nil {
			fmt.Println(err)
			return
		}
		// 创建CSV文件并写入数据
		csvFile, err := os.Create(filePath + "_" + sheetName + ".csv")
		if err != nil {
			fmt.Println(err)
			return
		}
		defer csvFile.Close()
		writer := csv.NewWriter(csvFile)
		defer writer.Flush()
		// 遍历行并写入CSV
		for _, row := range rows {
			if err := writer.Write(row); err != nil {
				fmt.Println(err)
				return
			}
		}

	}
}
