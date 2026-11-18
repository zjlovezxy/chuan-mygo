//go:build js && wasm

package app

import (
	"encoding/json"
	"errors"
	"syscall/js"

	"github.com/zjlovezxy/chuan-mygo/internal/transfer"
)

// Settings is what survives a restart. In the browser it lives in
// localStorage.
type Settings struct {
	Server      string `json:"server"`
	DownloadDir string `json:"download_dir"`
	Dark        bool   `json:"dark"`
}

const settingsKey = "chuan.settings"

func loadSettings() Settings {
	var cfg Settings
	raw := js.Global().Get("localStorage").Call("getItem", settingsKey)
	if raw.Type() != js.TypeString {
		return cfg
	}
	_ = json.Unmarshal([]byte(raw.String()), &cfg)
	return cfg
}

func saveSettings(cfg Settings) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return
	}
	js.Global().Get("localStorage").Call("setItem", settingsKey, string(data))
}

func defaultDownloadDir() string { return "浏览器下载目录" }

func newSink(string) transfer.Sink { return transfer.NewBrowserSink() }

func canChooseFolder() bool { return false }

func openFolderButtonAvailable() bool { return false }

func openFolder(string) error { return nil }

func chooseFolder(string) (string, error) {
	return "", errors.New("浏览器版本不能选择目录")
}
