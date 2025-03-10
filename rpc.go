package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/code-slammer/slammer-core/rpc"
)

func send_code(socket_path, language, code string) (*rpc.CodeOutput, error) {
	code_chan := make(chan *rpc.CodeOutput, 1)
	ctx := context.Background()
	err := rpc_listen(ctx, socket_path, coderunner_handler(ctx, language, code, code_chan))
	if err != nil {
		return nil, err
	}
	return <-code_chan, nil
}

func rpc_listen(ctx context.Context, socket_path string, handleConnection func(net.Conn) error) error {
	listener, err := net.Listen("unix", socket_path)

	if err != nil {
		log.Fatalf("Failed to listen on socket: %v", err)
	}
	defer listener.Close()

	// change the permissions to 777 on the socket path
	// so that the jailer can connect to it
	// this is a security risk, but it's the only way to make it work atm
	if err := os.Chmod(socket_path, 0777); err != nil {
		return fmt.Errorf("failed to change socket permissions: %v", err)
	}

	fmt.Println("Listening on ", socket_path)
	// there's no loop here because we only accept one connection
	conn, err := listener.Accept()
	if err != nil {
		select {
		case <-ctx.Done():
			return nil
		default:
			return fmt.Errorf("failed to accept connection: %v", err)
		}
	}

	return handleConnection(conn)
}

func coderunner_handler(ctx context.Context, language, code string, resp chan<- *rpc.CodeOutput) func(net.Conn) error {
	return func(conn net.Conn) error {
		defer conn.Close()
		defer close(resp)
		fmt.Printf("Handling connection from %s\n", conn.RemoteAddr().Network())
		// wait for the "ready" message
		// send the code
		// wait for the "done" message
		// read the output
		// send the output
		const TIMEOUT = 30 * time.Second
		const MAX_RESPONSE_SIZE = 4096

		conn.SetDeadline(time.Now().Add(TIMEOUT))

		serializer := rpc.Default_Serializer
		{ // wait for the "ready" message
			buf := make([]byte, 6)
			n, err := io.ReadFull(conn, buf)
			if err != nil {
				return err
			}
			if n != 6 || string(buf) != "ready\n" {
				return fmt.Errorf("expected 'ready', got %s", string(buf))
			}
			fmt.Println("Received 'ready'")
		}
		{ // send the code
			codeObj := rpc.Code{
				Type: language,
				Code: code,
			}
			err := serializer.Serialize(conn, &codeObj)
			if err != nil {
				return err
			}
			fmt.Println("Sent code")
		}
		{ // wait for the response
			var response rpc.CodeOutput
			// make a limited buffer to prevent DoS
			err := serializer.Deserialize(io.LimitReader(conn, MAX_RESPONSE_SIZE), &response)
			if err != nil {
				return err
			}
			fmt.Println("Received response")
			resp <- &response
		}

		return nil
	}
}
