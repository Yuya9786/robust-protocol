package main

import (
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
	Buf [][]*Packet
	window *WindowManager
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
	}
	c.Buf = make([][]*Packet, 1000)
	c.window = NewWindowManager()
}

func (c *Client) Send() {
	for {
		var packetIdent *FileIdent
		select {
			case packetIdent = <- c.Ch2:
				if c.Read(packetIdent) {
					continue
				}
				transID := c.window.push(packetIdent)
				packet := c.Buf[packetIdent.Fileno][packetIdent.Offset]
				packet.Header.TransID = transID
				data, err := packet.Serialize()
				if err != nil {
					panic(err)
				}

				c.Conn.Write(data)

			case packetIdent = <- c.Ch1:
				if c.Read(packetIdent) {
					continue
				}
				transID := c.window.push(packetIdent)
				packet := c.Buf[packetIdent.Fileno][packetIdent.Offset]
				packet.Header.TransID = transID
				data, err := packet.Serialize()
				if err != nil {
					panic(err)
				}

				c.Conn.Write(data)
		}
	}
}

func (c *Client) ReadFile() {
	for i:=0; i<1000; i++ {
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
					TransID: 0,
				},
				Data: packetData,
			}
			j++
			c.Buf[i] = append(c.Buf[i], packet)
			c.Ch1 <- &fileIdent
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

		if buf[0] != byte(1) {
			return
		}

		var packet Packet
		fmt.Println(buf[0:n])
		packet.Deserialize(buf[0:n])
		c.AckSegment(packet.Header.TransID)
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
		fmt.Println(buf)
		err := packet.Deserialize(buf)
		if err != nil {
			panic(err)
		}
		s.Bfp.Set(&packet)
		s.Ack(&packet)
	}
}

func (s *Server) Ack(receivedPacket *Packet) {
	// パケットごとにACKを返す
	ident := receivedPacket.Header.FileIdent
	transID := receivedPacket.Header.TransID

	p := Packet{
		Header: Header{
			Type:      1,
			Length:    0,
			Space:     0,
			FileIdent: ident,
			TransID: transID,
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