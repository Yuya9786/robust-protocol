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
	f := flag.Int("mode", 1, "1: server 2: client")
	flag.Parse()
	if *f == 1 {
		fmt.Println("mode: server")
	} else {
		fmt.Println("mode: client")
	}

	if *f == 1 {
		server()
	} else {
		client()
	}
}

func server() {
	s := &Server{}
	s.Initialize(TARO, HANAKO)

	go s.Receive()

	go s.handleClient()

	for {}
}

func client() {
	c := &Client {}
	c.Initialize(HANAKO, TARO)

	go c.ReadFile()
	go c.Send()

	go c.Receive()
	go c.HandleAck()

	time.Sleep(60)
}


