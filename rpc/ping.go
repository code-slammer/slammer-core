package rpc

type PingArgs struct{}
type PingReply struct {
	Msg string
}

func (*VMService) Ping(args PingArgs, reply *PingReply) error {
	reply.Msg = "pong"
	return nil
}
