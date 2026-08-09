package utils

import (
	"bytes"
	"context"
	"os"
	"runtime"

	"github.com/SebastiaanKlippert/go-wkhtmltopdf"
)

type PdfOption struct {
	FileName  string
	CoverPath string
	PageSize  string
	Toc       bool
}

func (p *PdfOption) GenPdf(buf *bytes.Buffer) (err error) {
	return p.GenPdfContext(context.Background(), buf)
}

func (p *PdfOption) GenPdfContext(ctx context.Context, buf *bytes.Buffer) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	wkhtmltopdf.SetPath(WkToPdfDir)
	pdfg, err := wkhtmltopdf.NewPDFGenerator()
	if err != nil {
		return err
	}
	page := wkhtmltopdf.NewPageReader(buf)
	page.FooterFontSize.Set(10)
	page.FooterRight.Set("[page]")
	page.DisableSmartShrinking.Set(true)

	page.EnableLocalFileAccess.Set(true)
	pdfg.AddPage(page)

	if p.CoverPath != "" {
		pdfg.Cover.EnableLocalFileAccess.Set(true)

		if runtime.GOOS == "windows" {
			pdfg.Cover.Input = p.CoverPath
		} else {
			pdfg.Cover.Input = "file://" + p.CoverPath
		}
	}

	pdfg.Dpi.Set(300)
	if p.Toc {
		pdfg.TOC.Include = true
		pdfg.TOC.TocHeaderText.Set("目 录")
		pdfg.TOC.HeaderFontSize.Set(18)

		pdfg.TOC.TocLevelIndentation.Set(15)
		pdfg.TOC.TocTextSizeShrink.Set(0.9)
		pdfg.TOC.DisableDottedLines.Set(false)
		pdfg.TOC.EnableTocBackLinks.Set(true)
	}

	pdfg.PageSize.Set(wkhtmltopdf.PageSizeA4)

	pdfg.MarginTop.Set(15)
	pdfg.MarginBottom.Set(15)
	pdfg.MarginLeft.Set(15)
	pdfg.MarginRight.Set(15)
	err = pdfg.CreateContext(ctx)
	if err != nil {
		return
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Write buffer contents to file on disk
	err = pdfg.WriteFile(p.FileName)
	if err != nil {
		return
	}
	if p.CoverPath != "" {
		err = os.Remove(p.CoverPath)
	}
	return
}
