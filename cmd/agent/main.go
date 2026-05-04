package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/code-slammer/slammer-core/internal/agentserver"
	"github.com/mdlayher/vsock"
)

func main() {
	workspace := flag.String("workspace", "/workspace", "allowed workspace for file and exec jobs")
	port := flag.Uint("vsock-port", 1024, "vsock port to listen on")
	maxBody := flag.Int64("max-body-bytes", agentserver.DefaultMaxBodyBytes, "maximum POST /jobs body size")
	maxFile := flag.Int64("max-file-bytes", agentserver.DefaultMaxFileBytes, "maximum single write_file payload size")
	maxOutput := flag.Int64("max-output-bytes", agentserver.DefaultMaxOutputBytes, "maximum captured stdout or stderr bytes")
	shutdown := flag.Bool("shutdown-after-jobs", false, "exit after the first jobs request")
	flag.Parse()

	listener, err := vsock.Listen(uint32(*port), nil)
	if err != nil {
		log.Fatalf("listen vsock: %v", err)
	}
	defer listener.Close()

	var httpServer *http.Server
	agent := agentserver.New(agentserver.Config{
		Workspace:         *workspace,
		MaxBodyBytes:      *maxBody,
		MaxFileBytes:      *maxFile,
		MaxOutputBytes:    *maxOutput,
		DefaultTimeout:    30 * time.Second,
		DefaultUID:        os.Getuid(),
		DefaultGID:        os.Getgid(),
		ShutdownAfterJobs: *shutdown,
		Shutdown: func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if httpServer != nil {
				_ = httpServer.Shutdown(ctx)
			}
		},
	})

	httpServer = &http.Server{
		Handler:           agent.Handler(),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       35 * time.Second,
		WriteTimeout:      35 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}

	log.Printf("agent listening on vsock port %d", *port)
	if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}
