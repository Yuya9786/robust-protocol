package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"sync"
	"time"
)

type Mypro struct {
	Type int8
	Length int16
	Space int8
	FileIdent FileIdent
	Data []byte		// <= MTU - 20 - 8 - 8
}

type FileIdent struct {
	Fileno int16
	Offset int16
}

type RetransCtrl struct {
	mu sync.Mutex
	v map[FileIdent]bool
}

func (ctrl *RetransCtrl) Set(ident FileIdent) {
	ctrl.mu.Lock()
	ctrl.v[ident] = false
	ctrl.mu.Unlock()
}

func (ctrl *RetransCtrl) Read(ident FileIdent) bool {
	ctrl.mu.Lock()
	f := ctrl.v[ident]
	ctrl.mu.Unlock()
	return f
}

func (ctrl *RetransCtrl) Ack(ident FileIdent) {
	ctrl.mu.Lock()
	ctrl.v[ident] = true
	ctrl.mu.Unlock()
}

const (
	MTU = 1500
	filesize = 100_000
	packet_data_size = MTU - 20 - 8 - 8
)

func main() {
	f := flag.Int("mode", 1, "1: server 2: client")
	flag.Parse()
	fmt.Println(*f)
	if *f == 1 {
		server()
	} else {
		client()
	}


}

func server() {
	srcaddr, err := net.ResolveUDPAddr("udp", "0.0.0.0:8888")
	if err != nil {
		panic(err)
	}

	conn, err := net.ListenUDP("udp", srcaddr)
	if err != nil {
		panic(err)
	}

	for {
		handleClient(conn)
	}
}

func handleClient(conn *net.UDPConn) {
	buf := make([]byte, 512, 512)

	n, addr, err := conn.ReadFromUDP(buf[0:])
	if err != nil {
		panic(err)
	}

	fmt.Println(string(buf[0:n]))

	daytime := time.Now().String()
	conn.WriteToUDP([]byte(daytime), addr)
}

func client() {
	start := time.Now()
	retransCtrl := RetransCtrl{
		v: make(map[FileIdent]bool),
	}

	ch1 := make(chan []byte, 100)
	var i int16
	for i=0; i<100; i++ {
		go readFile(ch1, "sampleA.txt", i, &retransCtrl)
	}

	udpAddr, err := net.ResolveUDPAddr("udp", "localhost:8888")
	if err != nil {
		panic(err)
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		panic(err)
	}
	go send(ch1, conn, &retransCtrl)

	go receive(conn, &retransCtrl)

	for {
		end := time.Now()
		if end.Sub(start).Milliseconds() >= 60_000 {
			return
		}
		time.Sleep(2)
	}

}

func readFile(ch chan []byte, filename string, number int16, retransCtrl *RetransCtrl) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatal(err)
	}
	for i:=0; i < len(data); i += (packet_data_size) {
		var data_size int16
		if i + packet_data_size > len(data) {
			data_size = int16(len(data) - i)
		} else {
			data_size = packet_data_size
		}
		packet_data := data[i:i+int(data_size)]
		fileIdent := FileIdent{
			Fileno: number,
			Offset: int16(i),
		}
		myprotcol_packet := Mypro {
			Type: 0,
			Length: data_size,
			Space: 0,
			FileIdent: fileIdent,
			Data: packet_data,
		}
		retransCtrl.Set(fileIdent)

		buf := bytes.NewBuffer(make([]byte, 0))
		binary.Write(buf, binary.BigEndian, &myprotcol_packet)
		data := buf.Bytes()
		fmt.Printf("%v\n", buf)
		ch <- data
	}
}


func send(ch chan []byte, conn *net.UDPConn, retransCtrl *RetransCtrl) {
	for {
		packetData := <- ch
		//fmt.Println(packetData)
		var fileNo, offSet int16
		buf := bytes.NewReader(packetData[4:6])
		binary.Read(buf, binary.BigEndian, &fileNo)
		buf = bytes.NewReader(packetData[6:8])
		binary.Read(buf, binary.BigEndian, &offSet)
		fileIdent := FileIdent{
			Fileno: fileNo,
			Offset: offSet,
		}

		if retransCtrl.Read(fileIdent) {
			continue
		}

		//fmt.Printf("sendFile Number: %v, Offset: %v", packetData[4:6], packetData[6:8])
		conn.Write(packetData)
		ch <- packetData
	}
}

func receive(conn *net.UDPConn, retransctrl *RetransCtrl) {
	for {
		buf := make([]byte, 1500, 1500)
		conn.ReadFromUDP(buf[0:])
		if buf[0] != byte(1) {
			return
		}
		var fileNo, offSet int16
		data := bytes.NewReader(buf[4:6])
		binary.Read(data, binary.BigEndian, &fileNo)
		data = bytes.NewReader(buf[6:8])
		binary.Read(data, binary.BigEndian, &offSet)
		fileIdent := FileIdent{
			Fileno: fileNo,
			Offset: offSet,
		}
		retransctrl.Ack(fileIdent)
	}
}
