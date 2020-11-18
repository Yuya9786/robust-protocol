package main

import (
	"flag"
	"fmt"
	"time"
)
const (
	MTU = 1500
	filesize = 102400
	packet_data_size = MTU - 20 - 8 - 8
	TARO = "169.254.155.219:8888"
	HANAKO = "169.254.229.153:8888"
)

func main() {
	mode := flag.Int("mode", 1, "1: server 2: client")
	src := flag.String("src", TARO, "sender address")
	dst := flag.String("dst", HANAKO, "receiver address")
	flag.Parse()
	if *mode == 1 {
		fmt.Println("mode: server")
	} else {
		fmt.Println("mode: client")
	}

	if *mode == 1 {
		server(*src, *dst)
	} else {
		client(*src, *dst)
	}
}

func server(src string, dst string) {
	s := &Server{}
	s.Initialize(dst, src)

	go s.Receive()

	go s.handleClient()

	for {}
}

func client(src string, dst string) {
	c := &Client {}
	c.Initialize(dst, src)

	go c.ReadFile()
	go c.Send()

	go c.Receive()

	time.Sleep(time.Second * 60)
}


