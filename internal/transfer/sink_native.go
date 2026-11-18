//go:build !js

package transfer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zjlovezxy/chuan-mygo/internal/protocol"
)

// DiskSink writes received files into a directory.
type DiskSink struct {
	Dir string
}

// NewDiskSink builds a sink for dir. An empty dir means the user's
// Downloads folder.
func NewDiskSink(dir string) *DiskSink {
	if strings.TrimSpace(dir) == "" {
		dir = DefaultDownloadDir()
	}
	return &DiskSink{Dir: dir}
}

// DefaultDownloadDir is where received files land unless configured
// otherwise: ~/Downloads, ~/下载, or the home directory.
func DefaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	for _, name := range []string{"Downloads", "下载"} {
		p := filepath.Join(home, name)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return home
}

// Open creates the temporary file a transfer streams into.
func (s *DiskSink) Open(meta protocol.FileMetadata) (Receiver, error) {
	dir := s.Dir
	if strings.TrimSpace(dir) == "" {
		dir = DefaultDownloadDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("无法打开下载目录 %s：%w", dir, err)
	}
	name := SanitizeName(meta.Name)
	tmp := filepath.Join(dir, "."+name+".part")
	f, err := os.Create(tmp)
	if err != nil {
		return nil, fmt.Errorf("无法创建 %s：%w", tmp, err)
	}
	return &diskReceiver{dir: dir, name: name, tmp: tmp, file: f}, nil
}

type diskReceiver struct {
	dir  string
	name string
	tmp  string
	file *os.File
}

func (r *diskReceiver) Write(index int, data []byte) error {
	_, err := r.file.WriteAt(data, int64(index)*int64(protocol.ChunkSize))
	return err
}

func (r *diskReceiver) Commit() (string, error) {
	if err := r.file.Close(); err != nil {
		return "", err
	}
	final := UniquePath(r.dir, r.name)
	if err := os.Rename(r.tmp, final); err != nil {
		_ = os.Remove(r.tmp)
		return "", err
	}
	return final, nil
}

func (r *diskReceiver) Abort() {
	_ = r.file.Close()
	_ = os.Remove(r.tmp)
}

// UniquePath returns dir/name, or dir/name (1).ext and so on when that name
// is taken.
func UniquePath(dir, name string) string {
	candidate := filepath.Join(dir, name)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return filepath.Join(dir, name)
}
