package xlsxsheet

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestWriteContainsHeaderAndEscapedCell(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, "导出", []string{"邮箱", "gpt密码", "邮箱密码", "session"}, [][]string{
		{"a@b.com", "p&1", "q<2>", `{"k":"v"}`},
	}); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var sheet string
	for _, f := range zr.File {
		if f.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		sheet = string(body)
	}
	if sheet == "" {
		t.Fatal("missing sheet1.xml")
	}
	for _, want := range []string{"邮箱", "gpt密码", "a@b.com", "p&amp;1", "q&lt;2&gt;"} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("sheet missing %q", want)
		}
	}
}
