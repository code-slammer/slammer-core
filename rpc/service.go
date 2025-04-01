package rpc

// VMService is the service that will be used to communicate with the host
type VMService struct {
	ShutdownFn func()
}
