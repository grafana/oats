package common

import (
	"net"
)

func HostTCPPort() (string, error) {
	conn, err := net.Listen("tcp", ":0")
	if err != nil {
		return "", err
	}

	addr := conn.Addr().String()

	err = conn.Close()
	if err != nil {
		return "", err
	}

	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}	

	return port, nil
}
