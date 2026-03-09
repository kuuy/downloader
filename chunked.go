package downloader

import (
  "errors"
  "fmt"
  "io"
  "log"
  "net/http"
  "os"
  "strconv"
  "sync"
  "time"

  "gopkg.in/cheggaaa/pb.v1"
)

type Chunked struct {
  url       string
  client    *http.Client
  dir       string
  filename  string
  totalSize int64
  bar       *pb.ProgressBar
  workers   int
  enableBar bool
  startTime int64
}

type DownloadResult struct {
  Err    error
  Worker int
}

func New(url string, dir, filename string, workers int, enableBar bool) (chunked *Chunked) {
  chunked = &Chunked{
    url:       url,
    client:    &http.Client{},
    dir:       dir,
    filename:  filename,
    workers:   workers,
    enableBar: enableBar,
    startTime: time.Now().UnixMilli(),
  }
  return
}

func (d *Chunked) Download() (filePath string, err error) {
  resp, err := http.Head(d.url)
  if err != nil {
    return
  }
  defer resp.Body.Close()

  if resp.StatusCode != http.StatusOK {
    err = fmt.Errorf(
      "request error: status[%s] code[%d]",
      resp.Status,
      resp.StatusCode,
    )
    return
  }

  if resp.Header.Get("Accept-Ranges") != "bytes" {
    err = errors.New("chunks download not support")
    return
  }

  totalSize, err := strconv.ParseInt(resp.Header.Get("content-length"), 10, 64)
  if err != nil {
    return
  }

  if d.workers < 1 {
    d.workers = 1
  }

  d.totalSize = totalSize
  log.Println("content length", totalSize)

  err = os.MkdirAll(
    d.dir,
    os.ModePerm,
  )
  if err != nil {
    return
  }

  err = d.downloadChunks()
  if err != nil {
    return
  }

  if err = d.verifyFileIntegrity(); err != nil {
    fmt.Errorf("file integrity check failed: %w", err)
    return
  }

  return
}

func (d *Chunked) downloadChunks() (err error) {
  chunkSize := d.totalSize / int64(d.workers)

  if d.enableBar {
    d.bar = pb.New(d.workers).SetMaxWidth(100).Prefix("Downloading...")
    d.bar.ShowElapsedTime = true
    d.bar.Start()
  }
  defer func() {
    if d.enableBar {
      d.bar.Finish()
    }
  }()

  wg := &sync.WaitGroup{}
  wg.Add(d.workers)
  finishedChan := d.wait(wg)
  quitChan := make(chan bool)
  downloadResultChan := make(chan *DownloadResult, d.workers)
  for i := 0; i < d.workers; i++ {
    go func(i int) {
      defer wg.Done()

      select {
      case <-quitChan:
        return
      default:
      }

      chunkPath := fmt.Sprintf("%v/%v.%v", d.dir, d.filename, i)
      chunkStart := int64(i) * chunkSize
      chunkEnd := chunkStart + chunkSize - 1
      if i == d.workers-1 {
        chunkEnd = d.totalSize - 1
      }

      err = d.downloadChunkWithRetry(chunkPath, chunkStart, chunkEnd)
      if err != nil {
        fmt.Errorf("download chunks %d failed %v\n", i, err)
      }
      downloadResultChan <- &DownloadResult{Err: err, Worker: i}
    }(i)
  }

  for {
    select {
    case <-finishedChan:
      err = d.combineChunks()
      if err != nil {
        fmt.Errorf("combine chunks failed %v\n", err)
      }
      return
    case result := <-downloadResultChan:
      if result.Err != nil {
        close(quitChan)
        return result.Err
      }
      if d.enableBar {
        d.bar.Increment()
      }
    }
  }

  return
}

func (d *Chunked) downloadChunkWithRetry(chunkPath string, start, end int64) (err error) {
  for attempt := 0; attempt < 3; attempt++ {
    err = d.downloadChunk(chunkPath, start, end)
    if err == nil {
      return
    }
    log.Printf("[Chunked] Chunk download failed (attempt %d/%d): %v\n", attempt+1, 3, err)
  }
  return
}

func (d *Chunked) downloadChunk(chunkPath string, start, end int64) (err error) {
  req, err := http.NewRequest(http.MethodGet, d.url, nil)
  if err != nil {
    return fmt.Errorf("failed to create request: %w", err)
  }
  req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

  resp, err := d.client.Do(req)
  if err != nil {
    return fmt.Errorf("request failed: %w", err)
  }
  defer resp.Body.Close()

  if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
    return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
  }

  out, err := os.Create(chunkPath)
  if err != nil {
    return
  }
  defer out.Close()

  _, err = io.Copy(out, resp.Body)
  return
}

func (d *Chunked) combineChunks() (err error) {
  out, err := os.Create(fmt.Sprintf(
    "%s/%s",
    d.dir,
    d.filename,
  ))
  if err != nil {
    return
  }
  defer out.Close()

  for i := 0; i < d.workers; i++ {
    chunkPath := fmt.Sprintf("%v/%v.%v", d.dir, d.filename, i)
    var chunkFile *os.File
    chunkFile, err = os.Open(chunkPath)
    if err != nil {
      return
    }

    _, err = io.Copy(out, chunkFile)
    chunkFile.Close()
    if err != nil {
      return
    }

    if err = os.Remove(chunkPath); err != nil {
      return
    }
  }

  return
}

func (d *Chunked) verifyFileIntegrity() error {
  fileInfo, err := os.Stat(fmt.Sprintf(
    "%s/%s",
    d.dir,
    d.filename,
  ))
  if err != nil {
    return fmt.Errorf("failed to stat file: %w", err)
  }

  actualSize := fileInfo.Size()
  if actualSize != d.totalSize {
    return fmt.Errorf("file size mismatch: expected %d bytes, got %d bytes", d.totalSize, actualSize)
  }

  return nil
}

func (d *Chunked) wait(wg *sync.WaitGroup) chan bool {
  c := make(chan bool, 1)
  go func() {
    wg.Wait()
    c <- true
  }()
  return c
}
