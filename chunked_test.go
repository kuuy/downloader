package downloader

import (
  "os"
  "testing"
)

func TestDownload(t *testing.T) {
  chunked := New(
    "http://212.183.159.230/200MB.zip",
    "./download",
    "200MB.zip",
    5,
    true,
  )
  filePath, err := chunked.Download()
  if err != nil {
    t.Fatal(err)
  }
  os.RemoveAll(filePath)
}
