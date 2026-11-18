//go:build !js

package transfer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// FileSource streams one file from disk.
type FileSource struct {
	file *os.File
	name string
	size int64
}

// OpenSource opens path for sending.
func OpenSource(path string) (*FileSource, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("无法读取 %s：%w", filepath.Base(path), err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if info.IsDir() {
		_ = f.Close()
		return nil, fmt.Errorf("%s 是文件夹，暂不支持整目录发送", filepath.Base(path))
	}
	return &FileSource{file: f, name: filepath.Base(path), size: info.Size()}, nil
}

func (s *FileSource) Name() string { return s.name }

func (s *FileSource) Size() int64 { return s.size }

func (s *FileSource) Type() string { return mimeByName(s.name) }

func (s *FileSource) ReadAt(offset int64, length int) ([]byte, error) {
	buf := make([]byte, length)
	n, err := s.file.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:n], nil
}

func (s *FileSource) Close() error { return s.file.Close() }

// PickSources asks the user to choose files with the system dialog.
func PickSources(ctx context.Context) ([]Source, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:   mygo.CallerWindow(ctx),
		Title:    "选择要发送的文件",
		Multiple: true,
	})
	if err != nil {
		return nil, err
	}
	sources := make([]Source, 0, len(paths))
	for _, p := range paths {
		src, err := OpenSource(p)
		if err != nil {
			for _, s := range sources {
				_ = s.Close()
			}
			return nil, err
		}
		sources = append(sources, src)
	}
	return sources, nil
}
