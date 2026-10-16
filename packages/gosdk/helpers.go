package dauth

import (
	"net/http"
	"strings"
)

// detectImageContentType 按文件扩展名猜图片类型，猜不出再看魔数，最后兜底 jpeg。
// d-auth 只接受 jpeg/png/webp，所以这里也只会产出这三种之一。
func detectImageContentType(fileName string, data []byte) string {
	switch strings.ToLower(extOf(fileName)) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	}
	// 魔数兜底，避免调用方没给扩展名或给错。
	if ct := http.DetectContentType(data); strings.HasPrefix(ct, "image/") {
		if ct == "image/webp" || ct == "image/png" || ct == "image/jpeg" {
			return ct
		}
	}
	return "image/jpeg"
}

func extOf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i:]
	}
	return ""
}

// isAbsoluteURL 判断是否带 scheme（http:// 或 https://）。
func isAbsoluteURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
