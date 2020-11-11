package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"sync"
	"time"
)

type FileIdent struct {
	Fileno int16
	Offset int16
}

type Header struct {
	Type int8
	Length int16
	Space int8
	FileIdent FileIdent
}

type Packet struct {
	Header Header
	Data []byte		// <= MTU - 20 - 8 - 8
}


func (p *Packet) Serialize() ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0))
	// 構造体に可変長のフィールドがあるとbinary.Write/Readはうまく動かないらしい
	// tp.Header/Dataに分けて書き込む
	if err := binary.Write(buf, binary.BigEndian, p.Header); err != nil {
		return nil, fmt.Errorf("failed to write: %v", err)
	}
	if err := binary.Write(buf, binary.BigEndian, p.Data); err != nil {
		return nil, fmt.Errorf("failed to write: %v", err)
	}

	return buf.Bytes(), nil
}

func (p *Packet) Deserialize(buf []byte) error {
	var header Header
	reader := bytes.NewReader(buf)
	if err := binary.Read(reader, binary.BigEndian, &header); err != nil {
		return err
	}
	p.Header = header
	p.Data = buf[8:]
	return nil
}


type BuilderFromPacket struct {
	DataSegments map[FileIdent][]byte
	CurrentReceivedFileSize map[int16]int
}

func (b *BuilderFromPacket) Set(tp *Packet) {
	ident := tp.Header.FileIdent
	if _, ok := b.DataSegments[ident]; ok {
		return
	}
	b.DataSegments[ident] = tp.Data
	fileNum := ident.Fileno
	if _, ok := b.CurrentReceivedFileSize[fileNum]; !ok {
		b.CurrentReceivedFileSize[fileNum] = 0
	}
	b.CurrentReceivedFileSize[fileNum] += int(tp.Header.Length)
	if b.CurrentReceivedFileSize[fileNum] >= filesize {
		b.WriteFile(ident.Fileno)
	}
}

func (b *BuilderFromPacket) WriteFile(fileNumber int16) error {
	fileName := fmt.Sprintf("data/%d.txt", fileNumber)
	file, err := os.OpenFile(fileName, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer file.Close()

	data := make([]byte, 0, 1500)
	for i := 0;;i++ {
		ident := FileIdent{
			Fileno: fileNumber,
			Offset: int16(i),
		}
		dataSegment, ok := b.DataSegments[ident]
		if !ok {
			break
		}
		for _, v := range dataSegment {
			data = append(data, v)
		}
	}

	fmt.Println("write data on a file: ", fileNumber, len(data))

	file.Write(data)

	return nil
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
	start := time.Now()
	addr, err := net.ResolveUDPAddr("udp", "0.0.0.0:8888")
	if err != nil {
		panic(err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		panic(err)
	}

	ch := make(chan []byte, 1000)

	go receive(conn, ch)

	bfp := &BuilderFromPacket{
		DataSegments:            make(map[FileIdent][]byte),
		CurrentReceivedFileSize: make(map[int16]int),
	}

	go bfp.handleClient(conn, ch)

	for {
		end := time.Now()
		if end.Sub(start).Milliseconds() >= 60_000 {
			return
		}
	}
}

func (bfp *BuilderFromPacket) handleClient(conn *net.UDPConn, ch chan []byte) {
	//buf := make([]byte, 1500)
	//
	//n, err := conn.Read(buf[0:])
	//if err != nil {
	//	panic(err)
	//}

	for {
		buf := <-ch

		var packet Packet
		packet.Deserialize(buf)
		fmt.Println("fileNum: ", packet.Header.FileIdent.Fileno, "offset: ", packet.Header.FileIdent.Offset)

		bfp.Set(&packet)

		Ack(conn, &packet)
	}
}

func client() {
	start := time.Now()
	retransCtrl := RetransCtrl{
		v: make(map[FileIdent]bool),
	}

	ch1 := make(chan []byte, 1000)	// チャネルが短いとうまく動かない場合あり
	var i int16
	for i=0; i<3; i++ {
		go readFile(ch1, "sample.txt", i, &retransCtrl)
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

	// go receive(conn, &retransCtrl)

	for {
		end := time.Now()
		if end.Sub(start).Milliseconds() >= 60_000 {
			fmt.Println("Time up")
			return
		}
		time.Sleep(2)
	}

}

func readFile(ch chan []byte, filename string, number int16, retransCtrl *RetransCtrl) {
	fmt.Println("readfile")
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		panic(err)
	}
	var j int16 = 0
	for i:=0; i < len(data); i += packet_data_size {
		var dataSize int16
		if i + packet_data_size > len(data) {
			dataSize = int16(len(data) - i)
		} else {
			dataSize = packet_data_size
		}
		packetData := data[i:i+int(dataSize)]
		fileIdent := FileIdent{
			Fileno: number,
			Offset: j,
		}
		packet := &Packet {
			Header: Header {
				Type: 0,
				Length: dataSize,
				Space: 0,
				FileIdent: fileIdent,
			},
			Data: packetData,
		}
		retransCtrl.Set(fileIdent)
		// fmt.Println(packet)
		data, err := packet.Serialize()
		if err != nil {
			panic(err)
		}
		j++
		ch <- data
	}
}

func send(ch chan []byte, conn *net.UDPConn, retransCtrl *RetransCtrl) {
	for {
		packetData := <- ch
		fmt.Println(len(packetData))
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

		fmt.Printf("sendFile Number: %v, Offset: %v\n", packetData[4:6], packetData[6:8])
		conn.Write(packetData)
		ch <- packetData
	}
}

func receive(conn *net.UDPConn, ch chan []byte) {
	for {
		buf := make([]byte, 1500)

		n, err := conn.Read(buf[0:])
		if err != nil {
			panic(err)
		}

		ch <- buf[0:n]
	}
}

func receiveAck(conn *net.UDPConn, retransctrl *RetransCtrl) {
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

func Ack(conn *net.UDPConn, receivedPacket *Packet) {
	ident := receivedPacket.Header.FileIdent
	p := Packet{
		Header: Header{
			Type:      1,
			Length:    0,
			Space:     0,
			FileIdent: FileIdent{
				Fileno: ident.Fileno,
				Offset: ident.Offset,
			},
		},
		Data:   nil,
	}

	data, err := p.Serialize()
	if err != nil {
		panic("serialization error")
	}
	conn.Write(data)
}
