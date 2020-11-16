package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
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

// 受け取ったパケットからファイルを再構築する
type BuilderFromPacket struct {
	DataSegments map[FileIdent][]byte
	CurrentReceivedFileSize map[int16]int	// ファイルごとの送られてきたサイズ
	ExpectedFileSegemnt map[int16]FileIdent	// ファイルごとの次に送られてくるべきセグメント (for window control)
}

func (b *BuilderFromPacket) Set(tp *Packet) {
	ident := tp.Header.FileIdent
	if _, ok := b.DataSegments[ident]; ok {
		return
	}
	b.DataSegments[ident] = tp.Data
	if _, ok := b.CurrentReceivedFileSize[ident.Fileno]; !ok {
		b.CurrentReceivedFileSize[ident.Fileno] = 0
	}
	b.CurrentReceivedFileSize[ident.Fileno] += int(tp.Header.Length)
	if _, ok := b.ExpectedFileSegemnt[ident.Fileno]; !ok {
		b.ExpectedFileSegemnt[ident.Fileno] = FileIdent{
			Fileno: ident.Fileno,
			Offset: 0,
		}
	}
	if ident == b.ExpectedFileSegemnt[ident.Fileno] {
		// 期待していたパケットが届いたため，次に期待するパケットに変える
		for i:=1;i<72;i++ {
			tmpIdent := FileIdent{
				Fileno: ident.Fileno,
				Offset: ident.Offset + int16(i),
			}
			if _, ok := b.DataSegments[tmpIdent]; !ok {
				// まだ受け取っていないデータセグメントが見つかった
				b.ExpectedFileSegemnt[ident.Fileno] = tmpIdent
				break
			}
		}
	}
	if b.CurrentReceivedFileSize[ident.Fileno] >= filesize {
		b.WriteFile(ident.Fileno)
	}
}

func (b *BuilderFromPacket) WriteFile(fileNumber int16) error {
	fileName := fmt.Sprintf("data/data%d", fileNumber)
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
