//go:build !js

package app

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"

	"github.com/zjlovezxy/chuan-mygo/internal/transfer"
)

// Settings is what survives a restart.
type Settings struct {
	Server      string `json:"server"`
	DownloadDir string `json:"download_dir"`
	Dark        bool   `json:"dark"`
}

// settingsPath is the config file under the user's config directory.
func settingsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "chuan-mygo", "settings.json")
}

func loadSettings() Settings {
	var cfg Settings
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	return cfg
}

func saveSettings(cfg Settings) {
	path := settingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

func defaultDownloadDir() string { return transfer.DefaultDownloadDir() }

func newSink(dir string) transfer.Sink { return transfer.NewDiskSink(dir) }

// canChooseFolder says whether the platform can pick a directory.
func canChooseFolder() bool { return true }

// openFolderButtonAvailable says whether the UI shows a reveal button.
func openFolderButtonAvailable() bool { return true }

func openFolder(dir string) error { return mygo.Shell.OpenPath(dir) }

// chooseFolder opens the directory picker.
func chooseFolder(current string) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:       "选择接收文件的目录",
		DefaultPath: current,
		Directory:   true,
		ButtonLabel: "选择",
	})
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", nil
	}
	return paths[0], nil
}
