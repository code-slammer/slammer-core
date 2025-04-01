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

type VMClient struct {
	SocketPath string
	client     *rpc.Client
	conn       net.Conn
}

func NewVMClient(socket_path string) *VMClient {
	return &VMClient{
		SocketPath: socket_path,
	}
}

func (v *VMClient) ExecuteCommand(args *slammer_rpc.ExecArgs) (*slammer_rpc.ExecReply, error) {
	if v.client == nil {
		return nil, fmt.Errorf("not connected to VM")
	}
	resp := slammer_rpc.ExecReply{}
	err := v.client.Call("VMService.ExecCommand", args, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to call ExecCommand: %v", err)
	}
	return &resp, nil
}
func (v *VMClient) UploadFile(file_path string, contents []byte) error {
	if v.client == nil {
		return fmt.Errorf("not connected to VM")
	}
	resp := slammer_rpc.UploadFileReply{}
	err := v.client.Call("VMService.UploadFile", slammer_rpc.UploadFileArgs{
		FilePath:    file_path,
		Permissions: 0777,
		UID:         1000,
		GID:         1000,
		Contents:    contents,
	}, &resp)
	if err != nil {
		return fmt.Errorf("failed to call UploadFile: %v", err)
	}
	return nil
}
func (v *VMClient) Close() {
	if v.client != nil {
		v.client.Close()
		v.client = nil
	}
	if v.conn != nil {
		v.conn.Close()
		v.conn = nil
	}

}

func (v *VMClient) Connect(context context.Context, sleep_delay time.Duration) error {
	// wait for the socket to be ready
	connect := func() (net.Conn, *rpc.Client, error) {
		conn, err := net.Dial("unix", v.SocketPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to connect to socket: %v", err)
		}
		// send CONNECT <port_num>\n
		_, err = fmt.Fprintf(conn, "CONNECT %d\n", PORT_NUMBER)
		if err != nil {
			return conn, nil, fmt.Errorf("failed to send connect message: %v", err)
		}
		// OK <assigned_hostside_port>\n
		var assigned_port int
		fmt.Fscanf(io.LimitReader(conn, 1024), "OK %d\n", &assigned_port)
		if assigned_port == 0 {
			return conn, nil, fmt.Errorf("failed to connect. Assigned port is 0")
		}
		fmt.Printf("Assigned port: %d\n", assigned_port)
		rpcClient := rpc.NewClient(conn)
		if rpcClient == nil {
			return conn, nil, fmt.Errorf("failed to create RPC client")
		}
		reply := slammer_rpc.PingReply{}
		err = rpcClient.Call("VMService.Ping", slammer_rpc.PingArgs{}, &reply)
		if err != nil {
			return conn, rpcClient, fmt.Errorf("failed to ping VM: %v", err)
		}
		if reply.Msg != "pong" {
			return conn, rpcClient, fmt.Errorf("failed to ping VM: %s != \"pong\"", reply.Msg)
		}
		return conn, rpcClient, nil
	}
	// Retry the operation until it succeeds or the context is done
	for {
		select {
		case <-context.Done():
			return context.Err()
		default:
			conn, client, err := connect()
			if err == nil {
				v.client = client
				v.conn = conn
				return nil
			} else {
				if client != nil {
					client.Close()
				}
				if conn != nil {
					conn.Close()
				}
			}
			// Sleep for the specified delay before retrying
			time.Sleep(sleep_delay)
		}
	}
}
