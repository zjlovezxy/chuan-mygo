package transfer

import (
	"testing"

	"github.com/zjlovezxy/chuan-mygo/internal/protocol"
)

func TestTotalChunks(t *testing.T) {
	cases := []struct {
		size int64
		want int
	}{
		{0, 0},
		{1, 1},
		{int64(protocol.ChunkSize), 1},
		{int64(protocol.ChunkSize) + 1, 2},
		{3 * int64(protocol.ChunkSize), 3},
	}
	for _, c := range cases {
		if got := protocol.TotalChunks(c.size); got != c.want {
			t.Errorf("TotalChunks(%d) = %d, want %d", c.size, got, c.want)
		}
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"photo.png":             "photo.png",
		"../../etc/passwd":      "passwd",
		`C:\Users\a\report.pdf`: "report.pdf",
		"":                      "未命名文件",
		".":                     "未命名文件",
		"..":                    "未命名文件",
		"a\x00b.txt":            "a_b.txt",
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:                      "0 B",
		999:                    "999 B",
		1024:                   "1.0 KB",
		1536:                   "1.5 KB",
		1024 * 1024:            "1.0 MB",
		3 * 1024 * 1024 * 1024: "3.0 GB",
	}
	for in, want := range cases {
		if got := HumanBytes(in); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
