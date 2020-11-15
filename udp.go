package main

import "net"

func UDPInitialize(dstaddr string, srcaddr string) (*net.UDPConn, error) {
	dstAddr, err := net.ResolveUDPAddr("udp", dstaddr)
	if err != nil {
		return nil, err
	}
	srcAddr, err := net.ResolveUDPAddr("udp", srcaddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", srcAddr, dstAddr)
	if err != nil {
		return nil, err
	}

	return conn,nil
}
