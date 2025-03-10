package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"syscall"
	"time"

	"github.com/code-slammer/slammer-core/rpc"
	"github.com/mdlayher/vsock"
)

func timeFunc(f func()) {
	start := time.Now()
	f()
	end := time.Now()
	fmt.Printf("%s took: %s\n", runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name(), end.Sub(start))
}

var languages = map[string][]string{
	"bash":   {"/bin/bash", "-c"},
	"python": {"/usr/bin/python3", "-c"},
}

func main() {
	log.Println("Started init process")
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered from panic: ", r)
		}
	}()
	defer syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)

	// Due to the nature of the init process, all necessary setup will panic if it fails
	serializer := rpc.Default_Serializer
	// time.Sleep(50 * time.Millisecond)
	conn, err := vsock.Dial(vsock.Host, 1024, nil)
	must(err)
	log.Println("Connected to the host")
	defer conn.Close()
	_, err = conn.Write([]byte("ready\n"))
	must(err)
	// read the code
	code := rpc.Code{}
	must(serializer.Deserialize(conn, &code))
	// run the code

	// create output buffers
	stdout := bytes.NewBuffer(nil)
	stderr := bytes.NewBuffer(nil)
	lang := languages[code.Type]
	cmd := &exec.Cmd{}
	if len(lang) >= 2 {
		cmd = exec.Command(lang[0], append(lang[1:], code.Code)...)
	} else {
		cmd = exec.Command(lang[0], code.Code)
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: 1000,
			Gid: 1000,
		},
	}
	cmd.Dir = "/home/user"
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	out := rpc.CodeOutput{}
	err = cmd.Run()
	out.Stdout = stdout.String()
	out.Stderr = stderr.String()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			out.ExitCode = exitErr.ExitCode()
		}
		out.Error = err.Error()
	}

	// send the output
	must(serializer.Serialize(conn, out))
}

func dumpHTTPResp(resp *http.Response) {
	fmt.Println("Status: ", resp.Status)
	fmt.Println("StatusCode: ", resp.StatusCode)
	fmt.Println("Proto: ", resp.Proto)
	fmt.Println("ProtoMajor: ", resp.ProtoMajor)
	fmt.Println("ProtoMinor: ", resp.ProtoMinor)
	fmt.Println("Header: ", resp.Header)
	fmt.Println("ContentLength: ", resp.ContentLength)
	fmt.Println("TransferEncoding: ", resp.TransferEncoding)
	fmt.Println("Close: ", resp.Close)
	fmt.Println("Uncompressed: ", resp.Uncompressed)
	// Body
	body, err := io.ReadAll(resp.Body)
	must(err)
	fmt.Println("Body: ", string(body))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func run(cmd string, args ...string) {
	c := exec.Command(cmd, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		panic(err)
	}
}
