// Command chuan is the desktop client: a mygo native window over the
// transfer engine.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/zjlovezxy/chuan-mygo/internal/app"
)

func main() {
	a := app.New()

	mygo.App.WhenReady(func() {
		w := mygo.NewWindow(mygo.WindowOptions{
			Title:     "文件快传",
			Width:     980,
			Height:    720,
			MinWidth:  380,
			MinHeight: 560,
			StateKey:  "main",
			Content:   ui.View(a.View),
		})
		a.SetWindow(w)
		a.Init()
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
