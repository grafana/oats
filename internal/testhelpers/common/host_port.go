package common

import (
	"fmt"
	"math/rand"
	"net"
)

func HostTCPPort() (string, error) {
	var selectedPort int

	for trials := 0; trials < 10000; trials++ {
		trialPort := rand.Intn(65535)
		if trialPort <= 1024 {
			continue
		}

		conn, connErr := net.Listen("tcp", fmt.Sprintf(":%d", trialPort))
		if connErr != nil {
			continue
		}

		closeErr := conn.Close()
		if closeErr != nil {
			continue
		}

		selectedPort = trialPort
		break
	}

	if selectedPort == 0 {
		return "", fmt.Errorf("could not find open unprivileged port after 10,000 attempts")
	}

	return fmt.Sprintf("%d", selectedPort), nil
}
