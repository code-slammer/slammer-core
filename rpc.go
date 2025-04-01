package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"net/rpc"

	slammer_rpc "github.com/code-slammer/slammer-core/rpc"
)

const PORT_NUMBER = 1024

func send_code(socket_path, command, code string) (*slammer_rpc.ExecReply, error) {
	// wait for the socket to be ready
	conn, err := net.Dial("unix", socket_path)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to socket: %v", err)
	}
	defer conn.Close()

	// send CONNECT <port_num>\n
	_, err = fmt.Fprintf(conn, "CONNECT %d\n", PORT_NUMBER)
	if err != nil {
		return nil, fmt.Errorf("failed to send connect message: %v", err)
	}
	// OK <assigned_hostside_port>\n
	var assigned_port int
	fmt.Fscanf(io.LimitReader(conn, 1024), "OK %d\n", &assigned_port)
	fmt.Printf("Assigned port: %d\n", assigned_port)
	if assigned_port == 0 {
		return nil, fmt.Errorf("failed to connect. Assigned port is 0")
	}
	// send the code
	client := rpc.NewClient(conn)
	defer client.Close()

	resp := slammer_rpc.ExecReply{}
	err = client.Call("VMService.ExecCommand", slammer_rpc.ExecArgs{
		Command:        command,
		Args:           []string{"-c", code},
		UID:            1000,
		GID:            1000,
		WorkDir:        "/home/user",
		Env:            []string{},
		ShutdownOnExit: true,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to call ExecCommand: %v", err)
	}
	return &resp, nil
}

func wait_for_vm_service(context context.Context, socket_path string, sleep_delay time.Duration) error {
	// wait for the socket to be ready
	operation := func() error {
		conn, err := net.Dial("unix", socket_path)
		if err != nil {
			return fmt.Errorf("failed to connect to socket: %v", err)
		}
		defer conn.Close()
		// send CONNECT <port_num>\n
		_, err = fmt.Fprintf(conn, "CONNECT %d\n", PORT_NUMBER)
		if err != nil {
			return fmt.Errorf("failed to send connect message: %v", err)
		}
		// OK <assigned_hostside_port>\n
		var assigned_port int
		fmt.Fscanf(io.LimitReader(conn, 1024), "OK %d\n", &assigned_port)
		if assigned_port == 0 {
			return fmt.Errorf("failed to connect. Assigned port is 0")
		}
		return nil
	}
	// Retry the operation until it succeeds or the context is done
	for {
		select {
		case <-context.Done():
			return context.Err()
		default:
			err := operation()
			if err == nil {
				return nil
			}
			// Sleep for the specified delay before retrying
			time.Sleep(sleep_delay)
		}
	}
}
