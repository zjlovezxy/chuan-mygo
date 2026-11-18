package app

import (
	"github.com/egoist/mygo/ui"
)

// The design paints a zinc surface stack with a rose accent, the same palette
// the Revda app uses. theme() fills these once per frame so a render pass
// always sees one consistent set of values.
var (
	colAppBg     ui.Color // window chrome and sidebar: zinc-950
	colPanel     ui.Color // page body: zinc-900
	colCard      ui.Color // cards and rows on the panel
	colWell      ui.Color // inputs and inset blocks on the panel
	colLine      ui.Color // hairlines: zinc-800
	colText      ui.Color // primary text: zinc-50
	colMuted     ui.Color // secondary text: zinc-500
	colFaint     ui.Color // tertiary text and icons: zinc-600
	colAccent    ui.Color // rose-600
	colAccentDim ui.Color // rose fill at low alpha, for pills and tints
	colSuccess   ui.Color // emerald-400
	colWarn      ui.Color // amber-400
)

// theme is the palette for one frame.
func (a *App) theme(c *ui.Context) *ui.Theme {
	a.mu.RLock()
	dark := a.dark
	a.mu.RUnlock()

	var t *ui.Theme
	if dark {
		t = ui.DarkTheme()
		t.Dark = true
		colAppBg = ui.Hex("#09090b")
		colPanel = ui.Hex("#18181b")
		colCard = ui.Hex("#141417")
		colWell = ui.Hex("#0c0c0e")
		colLine = ui.Hex("#27272a")
		colText = ui.Hex("#fafafa")
		colMuted = ui.Hex("#a1a1aa")
		colFaint = ui.Hex("#71717a")
		colAccent = ui.Hex("#e11d48")
		colAccentDim = ui.Hex("#e11d48").Alpha(0.15)
		colSuccess = ui.Hex("#34d399")
		colWarn = ui.Hex("#fbbf24")

		t.Background = colAppBg
		t.Surface = ui.Hex("#1c1c20")
		t.SurfaceHover = ui.Hex("#27272a")
		t.SurfacePressed = ui.Hex("#3f3f46")
		t.Border = colLine
		t.Text = colText
		t.TextMuted = colMuted
		t.Accent = colAccent
		t.AccentHover = ui.Hex("#f43f5e")
		t.AccentPressed = ui.Hex("#be123c")
		t.AccentText = ui.Hex("#ffffff")
		t.Danger = ui.Hex("#f87171")
		t.Warning = colWarn
		t.Success = colSuccess
		t.Selection = colAccent.Alpha(0.28)
		t.Focus = colAccent.Alpha(0.75)
		t.Scrollbar = colText.Alpha(0.28)
	} else {
		t = ui.LightTheme()
		colAppBg = ui.Hex("#f4f4f5")
		colPanel = ui.Hex("#ffffff")
		colCard = ui.Hex("#fafafa")
		colWell = ui.Hex("#f4f4f5")
		colLine = ui.Hex("#e4e4e7")
		colText = ui.Hex("#18181b")
		colMuted = ui.Hex("#71717a")
		colFaint = ui.Hex("#a1a1aa")
		colAccent = ui.Hex("#e11d48")
		colAccentDim = ui.Hex("#e11d48").Alpha(0.10)
		colSuccess = ui.Hex("#059669")
		colWarn = ui.Hex("#b45309")

		t.Background = colAppBg
		t.Surface = ui.Hex("#ffffff")
		t.SurfaceHover = ui.Hex("#f4f4f5")
		t.SurfacePressed = ui.Hex("#e4e4e7")
		t.Border = colLine
		t.Text = colText
		t.TextMuted = colMuted
		t.Accent = colAccent
		t.AccentHover = ui.Hex("#be123c")
		t.AccentPressed = ui.Hex("#9f1239")
		t.AccentText = ui.Hex("#ffffff")
		t.Danger = ui.Hex("#dc2626")
		t.Warning = colWarn
		t.Success = colSuccess
		t.Selection = colAccent.Alpha(0.18)
		t.Focus = colAccent.Alpha(0.6)
		t.Scrollbar = colText.Alpha(0.25)
	}
	t.Radius = 8

	p := c.Preferences()
	if p.HighContrast {
		t.Border = t.Border.Mix(t.Text, 0.45)
		t.TextMuted = t.TextMuted.Mix(t.Text, 0.4)
		t.Focus.A = 255
		t.Scrollbar = t.Scrollbar.Alpha(1.6)
	}
	if s := p.TextScale; s > 0 && s != 1 {
		t.FontSize *= s
	}
	return t
}
