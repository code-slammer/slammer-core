package main

import (
	"fmt"
	"log"
	"syscall"

	"net/rpc"

	slammer_rpc "github.com/code-slammer/slammer-core/rpc"
	"github.com/mdlayher/vsock"
)

func main() {
	log.Println("Started init process")
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered from panic: ", r)
		}
	}()
	defer shutdown()

	rpc.Register(&slammer_rpc.VMService{ShutdownFn: shutdown})

	// Due to the nature of the init process, all necessary setup will panic if it fails
	conn, err := vsock.Listen(1024, nil)
	must(err)
	vsock_listener = conn
	defer conn.Close()
	fmt.Println("Listening on vsock port 1024")

	rpc.Accept(conn)
}

var vsock_listener *vsock.Listener

func shutdown() {
	fmt.Println("Shutting down init process")
	if vsock_listener != nil {
		vsock_listener.Close()
	}
	syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
