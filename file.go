package main

type File struct {
	fileno int16
	data []byte
	state []bool	// 各セグメントの送信状況
	segSize int
}

func NewFile(fileno int16, data []byte, segSize int) *File {
	return &File {
		fileno: fileno,
		data: data,
		state: make([]bool, (len(data)+segSize-1)/segSize),
		segSize: segSize,
	}
}

type FileSegment struct {
	fileno int16
	offset int16
}


