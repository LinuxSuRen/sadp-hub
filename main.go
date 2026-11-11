package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/linuxsuren/sadp-hub/internal/httpserver"
	"github.com/linuxsuren/sadp-hub/internal/version"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP 监听地址")
	showVersion := flag.Bool("version", false, "打印版本")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Version, version.Platform())
		return
	}

	srv, err := httpserver.NewServer()
	if err != nil {
		log.Fatalf("初始化服务失败: %v", err)
	}
	log.Printf("sadp-hub %s 启动，监听 %s", version.Version, *addr)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatalf("HTTP 服务退出: %v", err)
	}
}
