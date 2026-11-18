package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/zjlovezxy/chuan-mygo/internal/session"
	"github.com/zjlovezxy/chuan-mygo/internal/transfer"
)

// TestRenderPages draws every page offscreen so a broken layout shows up as a
// failing assertion instead of a blank window, and leaves the frames in a
// temp directory for eyeballing.
func TestRenderPages(t *testing.T) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" && os.Getenv("CHUAN_SHOTS") == "" {
		t.Skip("no display here; run under xvfb-run to render the pages")
	}

	dir := os.Getenv("CHUAN_SHOTS")
	if dir == "" {
		dir = filepath.Join(t.TempDir())
	}

	cases := []struct {
		name   string
		width  int
		height int
		setup  func(a *App)
	}{
		{name: "home-wide", width: 1120, height: 760, setup: func(a *App) {
			a.page = PageHome
		}},
		{name: "home-narrow", width: 420, height: 800, setup: func(a *App) {
			a.page = PageHome
		}},
		{name: "receiver-narrow", width: 420, height: 800, setup: func(a *App) {
			a.page = PageHome
			a.roleIdx = 1
			a.status = "输入对方给的 6 位取件码"
		}},
		{name: "transfer-wide", width: 1120, height: 760, setup: func(a *App) {
			a.page = PageTransfer
			a.roleIdx = 0
			a.state = session.StateConnected
			a.detail = "数据通道已打开，可以开始传输"
			a.order = []string{"file_1", "file_2"}
			a.progress["file_1"] = transfer.Progress{
				ID: "file_1", Name: "年度报告.pdf", Moved: 3 << 20, Total: 8 << 20, Speed: 2 << 20,
			}
			a.progress["file_2"] = transfer.Progress{
				ID: "file_2", Name: "旅行照片.zip", Moved: 154 << 20, Total: 154 << 20, Incoming: true,
			}
			a.logs = []string{"21:04:12 生成取件码 A3F7KQ", "21:04:40 对方已加入房间", "21:04:41 数据通道已打开"}
		}},
		{name: "settings-wide", width: 1120, height: 760, setup: func(a *App) {
			a.page = PageSettings
		}},
	}

	for _, c := range cases {
		app := New()
		app.roomCode = "A3F7KQ"
		app.status = "取件码已生成，把它发给对方"
		app.history = []historyItem{
			{Name: "年度报告.pdf", Size: 8 << 20, At: time.Date(2026, 11, 18, 21, 4, 0, 0, time.UTC)},
			{Name: "旅行照片.zip", Size: 154 << 20, Incoming: true, At: time.Date(2026, 11, 18, 20, 11, 0, 0, time.UTC)},
		}
		c.setup(app)
		img := ui.Render(app.View, c.width, c.height, 2)
		if img == nil {
			t.Fatalf("%s: render returned nil", c.name)
		}
		if colors := distinctColors(img); colors < 8 {
			t.Fatalf("%s: only %d colors, the page looks blank", c.name, colors)
		}
		path := filepath.Join(dir, c.name+".png")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		f, err := os.Create(path)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatalf("%s: encode: %v", c.name, err)
		}
		f.Close()
		t.Logf("%s -> %s", c.name, path)
	}
}

func distinctColors(img *image.RGBA) int {
	seen := map[color.RGBA]struct{}{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			r, g, bl, a := img.At(x, y).RGBA()
			seen[color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), uint8(a >> 8)}] = struct{}{}
			if len(seen) > 64 {
				return len(seen)
			}
		}
	}
	return len(seen)
}
