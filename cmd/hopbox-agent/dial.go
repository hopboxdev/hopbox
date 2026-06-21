package main

import (
	"net"
	"time"
)

func dial(addr string) (net.Conn, error) {
	return net.DialTimeout("tcp", addr, 10*time.Second)
}
