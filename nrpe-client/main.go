package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"os"

	"github.com/canonical/nrped/common"
	"github.com/droundy/goopt"
)

func getSocket(transport_type int, endpoint string, tcpAddr *net.TCPAddr) (net.Conn, error) {
	switch transport_type {
	case 0:
		return net.DialTCP("tcp", nil, tcpAddr)
	case 1:
		return tls.Dial("tcp", endpoint, nil)
	case 2:
		return nil, nil //implement it
	}
	return nil, nil
}

func prepareConnection(endpoint string, transport_type int) net.Conn {
	tcpAddr, err := net.ResolveTCPAddr("tcp4", endpoint)
	common.CheckError(err)
	conn, err := getSocket(transport_type, endpoint, tcpAddr)

	common.CheckError(err)

	if conn != nil {
		return conn
	}
	return nil
}

func main() {

	if len(os.Args) < 2 {
		fmt.Printf("%s -h for help\n", os.Args[0])
		os.Exit(1)
	}

	var host = goopt.String([]string{"-H", "--host"}, "127.0.0.1", "The remote host running NRPE-Server")
	var port = goopt.Int([]string{"-p", "--port"}, 5666, "The remote port on which the NRPE-server listens")
	var transport = goopt.Int([]string{"-t", "--transport"}, 0, "Transport type: 0 - clear, 1 - ssl, 2 -ssh")
	var command = goopt.String([]string{"-c", "--command"}, "version",
		"The check command defined in the nrpe.cfg file you would like to trigger")
	var args = goopt.Strings([]string{"-a", "--args"}, "-h", "The arguments to the command")

	goopt.Parse(nil)
	service := fmt.Sprintf("%s:%d", *host, *port)
	conn := prepareConnection(service, *transport)

	fmt.Println("Connected to ", conn.RemoteAddr().String())
	command_to_send := NewCommand(*command, *args...)
	fmt.Println("Command to send to NRPE client: ", command_to_send.toStatusLine())

	pkt_to_send := common.PrepareToSend(command_to_send.toStatusLine(), common.QUERY_PACKET) // Convert *command to a string
	err := common.SendPacket(conn, pkt_to_send)
	common.CheckError(err)
	response_from_command, err := common.ReceivePacket(conn)
	common.CheckError(err)
	fmt.Println(string(response_from_command.CommandBuffer[:]))
	os.Exit(int(response_from_command.ResultCode))
}
