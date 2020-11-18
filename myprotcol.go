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
	Ch1 chan *FileIdent
	Ch2 chan *FileIdent
	RetransCtrl *RetransCtrl
	Buf [][][]byte
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
	c.Ch1 = make(chan *FileIdent, 70000)	// チャネルが短いとうまく動かない場合あり
	c.Ch2 = make(chan *FileIdent, 70000)
	c.RetransCtrl = &RetransCtrl {
		v: make(map[FileIdent]bool),
		newest: make(map[int16]int16),
	}
	c.Buf = make([][][]byte, 1000)
}

func (c *Client) Send() {
	for {
		var packetIdent *FileIdent
		select {
			case packetIdent = <- c.Ch2:
				if c.Read(packetIdent) {
					continue
				}
				c.Conn.Write(c.Buf[packetIdent.Fileno][packetIdent.Offset])
				//fmt.Println("2", packetIdent)
				c.Ch2 <- packetIdent
			case packetIdent = <- c.Ch1:
				if c.Read(packetIdent) {
					continue
				}
				c.Conn.Write(c.Buf[packetIdent.Fileno][packetIdent.Offset])
				//fmt.Println("1", packetIdent)
				if packetIdent.Offset >= 69 {
					c.Ch1 <- packetIdent
				}
		}
	}
}

func (c *Client) ReadFile() {
	for i:=0; i<350; i++ {
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
			c.Set(&fileIdent)
			packet := &Packet{
				Header: Header{
					Type:      0,
					Length:    dataSize,
					Space:     0,
					FileIdent: fileIdent,
				},
				Data: packetData,
			}
			data, err := packet.Serialize()
			if err != nil {
				panic(err)
			}
			j++
			c.Buf[i] = append(c.Buf[i], data)
			c.Ch1 <- &fileIdent
		}

	}
}

func (c *Client) Receive() {
	for {
		buf := make([]byte, 1500)

		_, err := c.Conn.Read(buf[0:])
		if err != nil {
			panic(err)
		}

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
		c.Ack(&ackIdent)
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
	// パケットごとにACKを返す
	ident := receivedPacket.Header.FileIdent

	p := Packet{
		Header: Header{
			Type:      1,
			Length:    0,
			Space:     0,
			FileIdent: ident,
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
	newest map[int16]int16
}

func (c *Client) Set(ident *FileIdent) {
	c.RetransCtrl.mu.Lock()
	c.RetransCtrl.v[*ident] = false
	c.RetransCtrl.mu.Unlock()
}

func (c *Client) Read(ident *FileIdent) bool {
	c.RetransCtrl.mu.Lock()
	b := c.RetransCtrl.v[*ident]
	c.RetransCtrl.mu.Unlock()
	return b
}

func (c *Client) Ack(ident *FileIdent) {
	c.RetransCtrl.mu.Lock()

	// 既にACKが来ているかチェック，来ていれば捨てる
	if c.RetransCtrl.v[*ident] {
		//fmt.Println("Drop Ack ", ident)
		c.RetransCtrl.mu.Unlock()
		return
	}

	//fmt.Println("ACK: ", ident)
	var i int16
	if _, ok := c.RetransCtrl.newest[ident.Fileno]; !ok {
		// そのファイルの中で初めてACKが返ってきた場合
		c.RetransCtrl.newest[ident.Fileno] = ident.Offset
		i = 0
	} else {
		i = c.RetransCtrl.newest[ident.Fileno]
		if i >= ident.Offset {
			// 既に受け取ったACKのオフセットよりも小さい場合
			c.RetransCtrl.v[*ident] = true
			c.RetransCtrl.mu.Unlock()
			return
		}
		c.RetransCtrl.newest[ident.Fileno] = ident.Offset
	}

	for ; i<ident.Offset; i++ {
		tmpIdent := &FileIdent{
			Fileno: ident.Fileno,
			Offset: i,
		}
		if c.RetransCtrl.v[*tmpIdent] {
			continue
		}
		//fmt.Println("失敗: ", tmpIdent)
		c.Ch2 <- tmpIdent
	}
	c.RetransCtrl.v[*ident] = true
	c.RetransCtrl.mu.Unlock()
}
