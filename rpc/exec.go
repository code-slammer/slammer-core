package rpc

import (
	"bytes"
	"os/exec"
	"syscall"
)

// ExecArgs holds the command to execute and its arguments.
type ExecArgs struct {
	Command        string
	Args           []string
	UID            int
	GID            int
	WorkDir        string
	Env            []string
	ShutdownOnExit bool
}

// ExecReply holds the output and any error message from executing the command.
type ExecReply struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

func (v *VMService) ExecCommand(args ExecArgs, reply *ExecReply) error {
	defer func() {
		if args.ShutdownOnExit {
			if v.ShutdownFn != nil {
				go v.ShutdownFn() // Shutdown the VM after command execution
			}
		}
	}()
	cmd := exec.Command(args.Command, args.Args...)
	stdout := bytes.NewBuffer(nil)
	stderr := bytes.NewBuffer(nil)
	cmd.Dir = args.WorkDir
	cmd.Env = args.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(args.UID),
			Gid: uint32(args.GID),
		},
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	reply.Stdout = stdout.Bytes()
	reply.Stderr = stderr.Bytes()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			reply.ExitCode = exitErr.ExitCode()
			return nil
		}
		return err
	}
	return nil
}
