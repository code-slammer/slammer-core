package rpc

// VMService is the service that will be used to communicate with the VM
type VMService struct {
	ShutdownFn func()
}
