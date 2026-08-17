// Package xlsxsheet 用标准库写一份最小可用的 .xlsx（不引入 excelize）。
// 仅覆盖「表头 + 若干文本行」；单元格用 inlineStr，避免 sharedStrings。
package xlsxsheet

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const excelCellMaxRunes = 32767

// Write 把 headers + rows 写成一份可被 Excel 打开的 workbook。
// sheetName 为空时用 "Sheet1"；非法 sheet 名字符会被替换。
func Write(w io.Writer, sheetName string, headers []string, rows [][]string) error {
	if strings.TrimSpace(sheetName) == "" {
		sheetName = "Sheet1"
	}
	sheetName = sanitizeSheetName(sheetName)

	zw := zip.NewWriter(w)
	files := map[string]string{
		"[Content_Types].xml":        contentTypesXML,
		"_rels/.rels":                relsXML,
		"xl/workbook.xml":            workbookXML(sheetName),
		"xl/_rels/workbook.xml.rels": workbookRelsXML,
		"xl/worksheets/sheet1.xml":   worksheetXML(headers, rows),
	}
	for name, body := range files {
		fw, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			return err
		}
		if _, err := io.WriteString(fw, body); err != nil {
			_ = zw.Close()
			return err
		}
	}
	return zw.Close()
}

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
  <Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
</Types>`

const relsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`

const workbookRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`

func workbookXML(sheetName string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets>
    <sheet name="` + xmlEscape(sheetName) + `" sheetId="1" r:id="rId1"/>
  </sheets>
</workbook>`
}

func worksheetXML(headers []string, rows [][]string) string {
	cols := len(headers)
	for _, row := range rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	if cols > 0 {
		b.WriteString("<cols>")
		for i := 0; i < cols; i++ {
			width := 24.0
			switch i {
			case 0:
				width = 32
			case 3:
				width = 72
			}
			fmt.Fprintf(&b, `<col min="%d" max="%d" width="%.1f" customWidth="1"/>`, i+1, i+1, width)
		}
		b.WriteString("</cols>")
	}
	b.WriteString("<sheetData>")
	writeRow(&b, 1, headers)
	for i, row := range rows {
		writeRow(&b, i+2, padRow(row, cols))
	}
	b.WriteString("</sheetData></worksheet>")
	return b.String()
}

func padRow(row []string, cols int) []string {
	if len(row) >= cols {
		return row[:cols]
	}
	out := make([]string, cols)
	copy(out, row)
	return out
}

func writeRow(b *strings.Builder, rowNum int, cells []string) {
	fmt.Fprintf(b, `<row r="%d">`, rowNum)
	for i, cell := range cells {
		ref := colName(i) + itoa(rowNum)
		fmt.Fprintf(b, `<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`,
			ref, xmlEscape(clipExcelText(cell)))
	}
	b.WriteString(`</row>`)
}

func colName(index int) string {
	// 0-based → A, B, ... Z, AA, ...
	index++
	var letters []byte
	for index > 0 {
		index--
		letters = append([]byte{byte('A' + index%26)}, letters...)
		index /= 26
	}
	return string(letters)
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

func clipExcelText(s string) string {
	s = stripInvalidXML(s)
	if utf8.RuneCountInString(s) <= excelCellMaxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:excelCellMaxRunes])
}

func stripInvalidXML(s string) string {
	return strings.Map(func(r rune) rune {
		if r == 0x09 || r == 0x0A || r == 0x0D {
			return r
		}
		if r < 0x20 || r == 0xFFFE || r == 0xFFFF {
			return -1
		}
		return r
	}, s)
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func sanitizeSheetName(name string) string {
	replacer := strings.NewReplacer(":", "_", "\\", "_", "/", "_", "?", "_", "*", "_", "[", "_", "]", "_")
	name = replacer.Replace(strings.TrimSpace(name))
	if name == "" {
		name = "Sheet1"
	}
	if utf8.RuneCountInString(name) > 31 {
		name = string([]rune(name)[:31])
	}
	return name
}
