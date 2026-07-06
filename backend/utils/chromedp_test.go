package utils

import (
	"os"
	"testing"
)

func TestPrintToPdf(t *testing.T) {
	if os.Getenv("DEDAO_RUN_CHROMEDP_INTEGRATION") != "1" {
		t.Skip("set DEDAO_RUN_CHROMEDP_INTEGRATION=1 to run chromedp PDF integration test")
	}
	filename := "file.pdf"
	err := ColumnPrintToPDF("Pvz6E94NYDg2JjQemzVL3rAkWQjnwp", filename, nil)

	if err != nil {
		t.Fatal("PrintToPDF test is failure", err)
	} else {
		_ = os.Remove(filename)
	}
}
