// Command chuan-server is the signalling server bundled with this repo. It is
// protocol compatible with https://transfer.52python.cn, so browsers using the
// upstream client can pair with clients pointed at your own instance.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/zjlovezxy/chuan-mygo/internal/server"
)

func main() {
	addr := flag.String("addr", ":8080", "监听地址")
	static := flag.String("static", "", "静态文件目录（浏览器版构建产物，留空则只提供 API）")
	ttl := flag.Duration("ttl", time.Hour, "房间有效期")
	flag.Parse()

	s := server.New(server.Options{RoomTTL: *ttl, StaticDir: *static})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("文件快传信令服务器已启动: %s (静态目录: %q)", *addr, *static)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
