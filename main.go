// Command dennet serves the den.mkm.pub API (dennet server run) and is a
// kubectl that talks to it over its Unix socket (dennet get dens).
package main

import (
	"context"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/rest"
	kubectl "k8s.io/kubectl/pkg/cmd"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/term"

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
	socket := func() string {
		if o.Socket != "" {
			return o.Socket
		}
		// macOS and some ssh sessions have no XDG_RUNTIME_DIR.
		if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
			return filepath.Join(d, "dennet", "dennet.sock")
		}
		return filepath.Join(o.StateDir, "dennet.sock")
	}

	// The kubectl half ignores ~/.kube/config and $KUBECONFIG: it always
	// dials the dennet socket, which needs no token or TLS.
	devNull, host := os.DevNull, "http://dennet"
	flags := genericclioptions.NewConfigFlags(true)
	flags.KubeConfig, flags.APIServer = &devNull, &host
	flags.WrapConfigFn = func(c *rest.Config) *rest.Config {
		c.Dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket())
		}
		return c
	}
	// kubectl-* executables on $PATH are kubectl plugins, not dennet's, but
	// kubectl lists them in help and shell completion. It has no switch for
	// that, so hide $PATH while it looks.
	path := os.Getenv("PATH")
	os.Setenv("PATH", "")
	root := kubectl.NewKubectlCommand(kubectl.KubectlOptions{
		Arguments:   os.Args,
		ConfigFlags: flags,
		IOStreams:   genericiooptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr},
	})
	os.Setenv("PATH", path)
	pre := root.PersistentPreRunE
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == cobra.ShellCompRequestCmd {
			os.Setenv("PATH", "")
		}
		return pre(cmd, args)
	}
	if c, _, err := root.Find([]string{"plugin"}); err == nil {
		root.RemoveCommand(c)
	}
	help := root.HelpFunc() // drop the now empty plugin group header
	root.SetHelpFunc(func(c *cobra.Command, args []string) {
		out, b := c.OutOrStdout(), &strings.Builder{}
		c.SetOut(b)
		help(c, args)
		c.SetOut(out)
		io.WriteString(term.NewResponsiveWriter(out), strings.Replace(b.String(), "Subcommands provided by plugins:\n\n", "", 1))
	})
	root.Use, root.Short, root.Long = "dennet", "dennet serves and controls den sessions", ""
	root.PersistentFlags().StringVar(&o.StateDir, "state-dir", stateDir, "directory for the token, serving certificate and kubeconfig")
	root.PersistentFlags().StringVar(&o.Socket, "socket", "", "dennet Unix socket (default $XDG_RUNTIME_DIR/dennet/dennet.sock, or <state-dir>/dennet.sock without it)")

	run := &cobra.Command{
		Use:   "run",
		Short: "Run the dennet API server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o.Socket = socket()
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return server.Run(ctx, o)
		},
	}
	run.Flags().StringVar(&o.TCPAddr, "tcp", "", "also serve TLS on this address (e.g. 127.0.0.1:0), for stock kubectl and remote clients")
	srv := &cobra.Command{Use: "server", Short: "Manage the dennet API server"}
	srv.AddCommand(run)
	root.AddCommand(srv)

	root.SilenceErrors = true // CheckErr prints them, as kubectl does
	cmdutil.CheckErr(root.Execute())
}
