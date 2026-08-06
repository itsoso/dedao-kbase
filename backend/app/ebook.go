package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"

	"github.com/yann0917/dedao-gui/backend/services"
	"github.com/yann0917/dedao-gui/backend/utils"
)

func EbookDetail(enID string) (detail *services.EbookDetail, err error) {
	detail, err = getService().EbookDetail(enID)
	return
}

// EbookCommentList get ebook 评分&书评
// sort like_count
func EbookCommentList(enID, sort string, page, limit int) (list *services.EbookCommentList, err error) {
	list, err = getService().EbookCommentList(enID, sort, page, limit)
	return
}

// EbookShelfAdd 加入书架
func EbookShelfAdd(enIDs []string) (resp *services.EbookShelfAddResp, err error) {
	resp, err = getService().EbookShelfAdd(enIDs)
	return
}

// EbookShelfRemove 移出书架
func EbookShelfRemove(enIDs []string) (resp *services.EbookShelfAddResp, err error) {
	resp, err = getService().EbookShelfRemove(enIDs)
	return
}

func EbookInfo(enID string) (info *services.EbookInfo, err error) {
	token, err1 := getService().EbookReadToken(enID)
	if err1 != nil {
		err = err1
		return
	}

	info, err = getService().EbookInfo(token.Token)
	return
}

func EbookPage(ctx context.Context, enID string) (info *services.EbookInfo, svgContent utils.SvgContents, err error) {
	return ebookPageWithService(ctx, dedaoServiceFromContext(ctx), enID)
}

func ebookPageWithService(ctx context.Context, service *services.Service, enID string) (info *services.EbookInfo, svgContent utils.SvgContents, err error) {
	token, err1 := service.EbookReadToken(enID)
	if err1 != nil {
		err = err1
		return
	}

	info, err = service.EbookInfo(token.Token)
	if err != nil {
		return
	}
	wgp := utils.NewWaitGroupPool(5)
	total, curr := len(info.BookInfo.Orders), 0
	type pageResult struct {
		index   int
		content *utils.SvgContent
		err     error
	}
	results := make(chan pageResult, total)
	var chapterMap sync.Map
	for _, ebookToc := range info.BookInfo.Toc {
		key := ebookToc.Href
		href := strings.Split(ebookToc.Href, "#")
		if len(href) > 1 {
			key = href[0]
		}
		chapterMap.Store(key, ebookToc)
	}
	for i, order := range info.BookInfo.Orders {
		var progress Progress
		progress.Total = total
		curr++
		progress.Current = curr
		progress.Pct = curr * 100 / progress.Total
		value, ok := chapterMap.Load(order.ChapterID)
		if ok {
			progress.Value = value.(utils.EbookToc).Text
			chapterMap.Delete(order.ChapterID)
		}
		emitEbookDownloadProgress(ctx, progress)
		wgp.Add()
		go func(i int, order services.EbookOrders) {
			defer wgp.Done()
			index, count, offset := 0, 20, 0
			svgList, fetchErr := runEbookPageFetch(func() ([]string, error) {
				return generateEbookPagesWithService(service, order.ChapterID, token.Token, index, count, offset)
			})
			if fetchErr != nil {
				results <- pageResult{index: i, err: fetchErr}
				return
			}
			results <- pageResult{index: i, content: &utils.SvgContent{
				Contents:   svgList,
				ChapterID:  order.ChapterID,
				PathInEpub: order.PathInEpub,
				OrderIndex: i,
			}}
		}(i, order)
	}
	wgp.Wait()
	close(results)
	ordered := make([]*utils.SvgContent, total)
	for result := range results {
		if result.err != nil {
			if err == nil {
				err = result.err
			}
			continue
		}
		ordered[result.index] = result.content
	}
	if err != nil {
		return
	}
	for _, content := range ordered {
		if content != nil {
			svgContent = append(svgContent, content)
		}
	}
	return
}

func runEbookPageFetch(fetch func() ([]string, error)) (pages []string, err error) {
	defer func() {
		if recover() != nil {
			pages = nil
			err = fmt.Errorf("ebook page fetch failed")
		}
	}()
	if fetch == nil {
		return nil, fmt.Errorf("ebook page fetch is required")
	}
	return fetch()
}

func generateEbookPages(chapterID, token string, index, count, offset int) (svgList []string, err error) {
	return generateEbookPagesWithService(getService(), chapterID, token, index, count, offset)
}

func generateEbookPagesWithService(service *services.Service, chapterID, token string, index, count, offset int) (svgList []string, err error) {
	fmt.Printf("chapterID:%#v\n", chapterID)
	pageList, err := service.EbookPages(chapterID, token, index, count, offset)
	if err != nil {
		return
	}

	for _, item := range pageList.Pages {
		desContents, decryptErr := decryptEbookPage(item.Svg)
		if decryptErr != nil {
			return nil, fmt.Errorf("decrypt ebook page: %w", decryptErr)
		}
		svgList = append(svgList, desContents)
	}
	// fmt.Printf("IsEnd:%#v\n", pageList.IsEnd)
	if !pageList.IsEnd {
		index = count
		count += 20
		list, err1 := generateEbookPagesWithService(service, chapterID, token, index, count, offset)
		if err1 != nil {
			err = err1
			return
		}

		svgList = append(svgList, list...)
	}
	// FIXME: debug
	// err = utils.SaveFile(OutputDir, chapterID, "", strings.Join(svgList, "\n"))
	return
}

// PKCS7Unpad 实现PKCS7去填充
func PKCS7Unpad(data []byte) []byte {
	result, _ := pkcs7Unpad(data, aes.BlockSize)
	return result
}

// DecryptAES 实现AES - CBC解密
func DecryptAES(contents string) string {
	result, _ := decryptEbookPage(contents)
	return result
}

func decryptEbookPage(contents string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(contents)
	if err != nil {
		return "", fmt.Errorf("invalid base64 ciphertext")
	}

	key := []byte("3e4r06tjkpjcevlbslr3d96gdb5ahbmo")
	iv := []byte("6fd89a1b3a7f48fb")

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("initialize ebook cipher: %w", err)
	}

	blockSize := block.BlockSize()
	if len(ciphertext) == 0 || len(ciphertext)%blockSize != 0 {
		return "", fmt.Errorf("invalid ciphertext length")
	}
	mode := cipher.NewCBCDecrypter(block, iv[:blockSize])
	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)

	plaintext, err = pkcs7Unpad(plaintext, blockSize)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || blockSize <= 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("invalid padded plaintext length")
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize || padding > len(data) {
		return nil, fmt.Errorf("invalid ebook page padding")
	}
	for _, value := range data[len(data)-padding:] {
		if int(value) != padding {
			return nil, fmt.Errorf("invalid ebook page padding")
		}
	}
	return data[:len(data)-padding], nil
}
