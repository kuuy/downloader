package downloader

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestDownloadWithCustomHeader(t *testing.T) {
	headerKey := "X-Custom-Header"
	headerValue := "CustomValue"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(headerKey) != headerValue {
			t.Errorf("Expected header %s: %s, got: %s", headerKey, headerValue, r.Header.Get(headerKey))
		}

		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", "10")
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == http.MethodGet {
			rangeHeader := r.Header.Get("Range")
			if rangeHeader == "" {
				t.Errorf("Expected Range header")
				return
			}
			var start, end int64
			fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)

			content := "0123456789"
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte(content[start : end+1]))
			return
		}
	}))
	defer server.Close()

	dir := "./test_download"
	filename := "test.txt"
	defer os.RemoveAll(dir)

	customHeader := make(http.Header)
	customHeader.Set(headerKey, headerValue)

	chunked := New(server.URL, dir, filename, 2, false).WithHeader(customHeader)
	_, err := chunked.Download()
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	content, err := os.ReadFile(fmt.Sprintf("%s/%s", dir, filename))
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}

	if string(content) != "0123456789" {
		t.Errorf("Expected content '0123456789', got '%s'", string(content))
	}
}
