// Command dennet serves the den.mkm.pub API: an embedded Kubernetes-style API
// server that coordinates den sessions.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mkmik/dennet/internal/server"
)

func main() {
	// XDG base directories: token, certificate and kubeconfig are state;
	// the socket is runtime, next to den's $XDG_RUNTIME_DIR/den/ sockets.
	home, _ := os.UserHomeDir()
	stateDir := filepath.Join(home, ".local", "state", "dennet")
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		stateDir = filepath.Join(d, "dennet")
	}
	var o server.Options
	flag.StringVar(&o.StateDir, "state-dir", stateDir, "directory for the token, serving certificate and kubeconfig")
	flag.StringVar(&o.Socket, "socket", "", "Unix socket to serve on (default $XDG_RUNTIME_DIR/dennet/dennet.sock, or <state-dir>/dennet.sock without it)")
	flag.StringVar(&o.TCPAddr, "tcp", "127.0.0.1:0", "TLS address to serve on, for kubectl and remote clients; empty disables")
	flag.Parse()
	if o.Socket == "" {
		o.Socket = filepath.Join(o.StateDir, "dennet.sock")
		// macOS and some ssh sessions have no XDG_RUNTIME_DIR.
		if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
			o.Socket = filepath.Join(d, "dennet", "dennet.sock")
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.Run(ctx, o); err != nil {
		fmt.Fprintln(os.Stderr, "dennet:", err)
		os.Exit(1)
	}
}
