package dashboard

import (
	"embed"
	"errors"
	"strings"
)

//go:embed assets/*
var packaged embed.FS

// Asset is an explicitly packaged, trusted local resource. It is never a path to
// a host file. Only the three content types below are accepted by this service.
type Asset struct {
	ContentType string
	Data        []byte
}

func copyAssets(input map[string]Asset) (map[string]Asset, error) {
	if input == nil {
		input = make(map[string]Asset)
		for path, kind := range map[string]string{
			"/":           "text/html; charset=utf-8",
			"/session.js": "text/javascript; charset=utf-8",
		} {
			file := "session.js"
			if path == "/" {
				file = "index.html"
			}
			data, err := packaged.ReadFile("assets/" + file)
			if err != nil {
				return nil, errors.New("dashboard assets unavailable")
			}
			input[path] = Asset{kind, data}
		}
	}
	output := make(map[string]Asset, len(input))
	for path, asset := range input {
		if !assetPath(path) {
			return nil, errors.New("invalid dashboard asset path")
		}
		switch asset.ContentType {
		case "text/html; charset=utf-8", "text/javascript; charset=utf-8",
			"text/css; charset=utf-8":
		default:
			return nil, errors.New("invalid dashboard asset content type")
		}
		output[path] = Asset{asset.ContentType, append([]byte(nil), asset.Data...)}
	}
	if _, ok := output["/"]; !ok {
		return nil, errors.New("dashboard shell required")
	}
	return output, nil
}

func assetPath(path string) bool {
	if path == "/" {
		return true
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "/api/") ||
		path == "/api" {
		return false
	}
	for _, segment := range strings.Split(path[1:], "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, c := range segment {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
				c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
				return false
			}
		}
	}
	return true
}
