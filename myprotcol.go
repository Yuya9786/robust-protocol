package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"net"
	"sync"
)

type Client struct {
	Conn *net.UDPConn
	Ch1 chan []byte
	Ch2 chan []byte
	RetransCtrl *RetransCtrl
}

type Server struct {
	Conn *net.UDPConn
	Ch chan []byte
	Bfp *BuilderFromPacket
}

func (c *Client) Initialize(dstAddr string, srcAddr string) {
	conn, err := UDPInitialize(dstAddr, srcAddr)
	if err != nil {
		panic(err)
	}

	c.Conn = conn
	c.Ch1 = make(chan []byte, 70000)	// チャネルが短いとうまく動かない場合あり
	c.Ch2 = make(chan []byte, 2000)
	c.RetransCtrl = &RetransCtrl {
		v: make(map[FileIdent]bool),
	}
}

func (c *Client) Send() {
	for {
		packetData := <- c.Ch1
		var fileNo, offSet int16
		buf := bytes.NewReader(packetData[4:6])
		binary.Read(buf, binary.BigEndian, &fileNo)
		buf = bytes.NewReader(packetData[6:8])
		binary.Read(buf, binary.BigEndian, &offSet)
		fileIdent := FileIdent{
			Fileno: fileNo,
			Offset: offSet,
		}

		if c.RetransCtrl.Read(fileIdent) {
			continue
		}

		c.Conn.Write(packetData)
		c.Ch1 <- packetData
	}
}

func (c *Client) ReadFile() {
	for i:=0; i<200; i++ {
		fmt.Printf("read data%d\n", i)
		fileName := fmt.Sprintf("data/data%d", i)
		data, err := ioutil.ReadFile(fileName)
		if err != nil {
			panic(err)
		}
		var j int16 = 0
		for k := 0; k < len(data); k += packet_data_size {
			var dataSize int16
			if k+packet_data_size > len(data) {
				dataSize = int16(len(data) - k)
			} else {
				dataSize = packet_data_size
			}
			packetData := data[k : k+int(dataSize)]
			fileIdent := FileIdent{
				Fileno: int16(i),
				Offset: j,
			}
			packet := &Packet{
				Header: Header{
					Type:      0,
					Length:    dataSize,
					Space:     0,
					FileIdent: fileIdent,
				},
				Data: packetData,
			}
			c.RetransCtrl.Set(fileIdent)
			data, err := packet.Serialize()
			if err != nil {
				panic(err)
			}
			j++
			c.Ch1 <- data
		}

	}
}

func (c *Client) Receive() {
	for {
		buf := make([]byte, 1500)

		n, err := c.Conn.Read(buf[0:])
		if err != nil {
			panic(err)
		}
		c.Ch2 <- buf[0:n]
	}
}

func (c *Client) HandleAck() {
	for {
		buf := <-c.Ch2
		//fmt.Println(buf)
		if buf[0] != byte(1) {
			return
		}
		var fileNo, offSet int16
		data := bytes.NewReader(buf[4:6])
		binary.Read(data, binary.BigEndian, &fileNo)
		data = bytes.NewReader(buf[6:8])
		binary.Read(data, binary.BigEndian, &offSet)
		ackIdent := FileIdent{
			Fileno: fileNo,
			Offset: offSet,
		}
		c.RetransCtrl.Ack(ackIdent)
	}
}

func (s *Server) Initialize(dstAddr string, srcAddr string) {
	conn, err := UDPInitialize(dstAddr, srcAddr)
	if err != nil {
		panic(err)
	}

	ch := make(chan []byte, 1500)

	s.Conn = conn
	s.Ch = ch
	s.Bfp = &BuilderFromPacket {
		DataSegments:            make(map[FileIdent][]byte),
		CurrentReceivedFileSize: make(map[int16]int),
		ExpectedFileSegemnt: make(map[int16]FileIdent),
	}
}

func (s *Server) Receive() {
	for {
		buf := make([]byte, 1500)

		n, err := s.Conn.Read(buf[0:])
		if err != nil {
			panic(err)
		}
		s.Ch <- buf[0:n]
	}
}

func (s *Server) handleClient() {
	for {
		buf := <- s.Ch

		var packet Packet
		packet.Deserialize(buf)

		s.Bfp.Set(&packet)

		s.Ack(&packet)
	}
}

func (s *Server) Ack(receivedPacket *Packet) {
	ident := receivedPacket.Header.FileIdent

	ackIdent := s.Bfp.ExpectedFileSegemnt[ident.Fileno]

	p := Packet{
		Header: Header{
			Type:      1,
			Length:    0,
			Space:     0,
			FileIdent: ackIdent,
		},
		Data:   nil,
	}

	data, err := p.Serialize()
	if err != nil {
		panic("serialization error")
	}
	_, err = s.Conn.Write(data)
	if err != nil {
		panic(err)
	}
}


// 再送制御を担当
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
	// ackによって送られてきたidentよりも前のセグメントは送信完了と見なす
	for i:=0;int16(i) <= ident.Offset;i++ {
		tmpIdent := FileIdent{
			Fileno: ident.Fileno,
			Offset: int16(i),
		}
		ctrl.v[tmpIdent] = true
	}
	ctrl.mu.Unlock()
}