package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"syscall"
)

const MMDS_IP = "169.254.169.254"

type Code struct {
	Code string `json:"code"`
	Type string `json:"type"`
}

type CodeOutput struct {
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

func main() {
	fmt.Println("Started init process")
	defer syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)

	// Due to the nature of the init process, all necessary setup will panic if it fails
	setup_network()
	resp := getMMDS()
	if resp.Type != "bash" {
		fmt.Println("Invalid code type")
		return
	}
	// create a stdout buffer
	stdout := bytes.NewBuffer(nil)
	stderr := bytes.NewBuffer(nil)

	cmd := exec.Command("/bin/bash", "-c", resp.Code)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: 1000,
			Gid: 1000,
		},
	}
	cmd.Dir = "/home/user"
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	out := CodeOutput{}
	err := cmd.Run()
	out.Stdout = stdout.String()
	out.Stderr = stderr.String()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			out.ExitCode = exitErr.ExitCode()
		}
		out.Error = err.Error()
	}
	outMarshalled, err := json.Marshal(out)
	if err != nil {
		panic(err)
	}
	fmt.Printf("====>%s\n", string(outMarshalled))
}
func setup_network() {
	//ip addr add 172.16.0.2/24 dev eth0
	run("/sbin/ip", "addr", "add", "172.16.0.2/24", "dev", "eth0")
	//ip link set eth0 up
	run("/sbin/ip", "link", "set", "eth0", "up")
	// ip route add default via 172.16.0.1 dev eth0
	run("/sbin/ip", "route", "add", "default", "via", "172.16.0.1", "dev", "eth0")
	// ip route add 169.254.170.2 dev eth0
	run("/sbin/ip", "route", "add", MMDS_IP, "dev", "eth0")
}

func getMMDS() *Code {
	// Get MMDS
	token := getMMDSToken()

	req, err := http.NewRequest("GET", "http://"+MMDS_IP+"/", nil)
	must(err)
	req.Header.Add("X-metadata-token", string(token))
	req.Header.Add("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	must(err)
	respBody, err := io.ReadAll(resp.Body)
	must(err)
	code := Code{}
	must(json.Unmarshal(respBody, &code))
	return &code
}

func getMMDSToken() string {
	// Get MMDS
	fmt.Println("Getting MMDS")
	// fetch the api token
	req, err := http.NewRequest("PUT", "http://"+MMDS_IP+"/latest/api/token", nil)
	must(err)
	req.Header.Add("X-metadata-token-ttl-seconds", "60")
	resp, err := http.DefaultClient.Do(req)
	must(err)
	token, err := io.ReadAll(resp.Body)
	must(err)
	return string(token)
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
