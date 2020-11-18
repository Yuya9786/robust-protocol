package main

type TransSegment struct {
	segment *FileIdent
	ack		bool
}

type WindowManager struct {
	window []TransSegment
	transHead uint32
}

func NewWindowManager() *WindowManager {
	return &WindowManager{
		window: make([]TransSegment, 0),
		transHead: 0,
	}
}

func (wm *WindowManager) push(segment *FileIdent) uint32  {
	transId := wm.transHead + uint32(len(wm.window))

	wm.window = append(wm.window, TransSegment{
		segment: segment,
		ack:     false,
	})

	return transId
}

func (c *Client) AckSegment(transID uint32) {

	idxWindow := int(transID) - int(c.window.transHead)
	if idxWindow >=0 && idxWindow < len(c.window.window) && !c.window.window[transID].ack {
		c.window.window[transID].ack = true
	}

	for i:=c.window.transHead; int(i) < idxWindow; i++ {
		item := c.window.window[0]
		c.window.window = c.window.window[1:]
		c.window.transHead++
		
		if !item.ack {
			if !c.Read(item.segment) {
				transID := c.window.push(item.segment)
				packet := c.Buf[item.segment.Fileno][item.segment.Offset]
				packet.Header.transID = transID
				data, err := packet.Serialize()
				if err != nil {
					panic(err)
				}

				c.Conn.Write(data)
			}
		}
	}
}