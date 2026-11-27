// Command dennet serves the den.mkm.pub API (dennet server run) and is a
// kubectl that talks to it over its Unix socket (dennet get dens).
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/rest"
	"k8s.io/component-base/cli"
	kubectl "k8s.io/kubectl/pkg/cmd"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/term"

	"mkm.pub/dennet/internal/server"
	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

func main() {
	// XDG base directories: token, certificate, kubeconfig and etcd data are state;
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
	// denClient talks to the den.mkm.pub API with dennet's own types.
	denClient := func() (*rest.RESTClient, error) {
		cfg, err := flags.ToRESTConfig()
		if err != nil {
			return nil, err
		}
		cfg.APIPath, cfg.GroupVersion = "/apis", &denv1.SchemeGroupVersion
		cfg.NegotiatedSerializer = server.Codecs.WithoutConversion()
		return rest.RESTClientFor(cfg)
	}
	// surfaceOf is the terminal Surface that shows the den, or nil.
	// ponytail: lists the namespace's Surfaces on each call; cache per
	// namespace if get dens -o space on many dens gets slow.
	surfaceOf := func(ctx context.Context, ns, den string) (*denv1.Surface, error) {
		c, err := denClient()
		if err != nil {
			return nil, err
		}
		var l denv1.SurfaceList
		if err := c.Get().Namespace(ns).Resource("surfaces").Do(ctx).Into(&l); err != nil {
			return nil, err
		}
		for _, s := range l.Items {
			if s.Spec.Terminal != nil && s.Spec.Terminal.Den == den {
				return &s, nil
			}
		}
		return nil, nil
	}
	// Inside a den, @ names the den this command runs in, like $WEZTERM_PANE
	// for wezterm cli: den @, surface @ (the Surface showing it) and space @
	// (that Surface's Space), also as den/@, and logs @. $DEN_SOCKET is
	// <dir>/<namespace>/<den>.sock (socketPath in den). Each @ is replaced in
	// args itself: cobra hands the same slice to Run.
	self := func(cmd *cobra.Command, args []string) error {
		for i, a := range args {
			typ, name, slash := strings.Cut(a, "/")
			if !slash {
				typ, name = "", a
				if i > 0 {
					typ = args[i-1]
				} else if cmd.Name() == "logs" {
					typ = "den"
				}
			}
			if name != "@" {
				continue
			}
			sock := os.Getenv("DEN_SOCKET")
			if sock == "" {
				return fmt.Errorf("@ is the den this runs in, but $DEN_SOCKET is not set")
			}
			ns, den := filepath.Base(filepath.Dir(sock)), strings.TrimSuffix(filepath.Base(sock), ".sock")
			if f := cmd.Flag("namespace"); f != nil && f.Changed && f.Value.String() != ns {
				return fmt.Errorf("@ is in namespace %s, not %s", ns, f.Value.String())
			}
			*flags.Namespace = ns
			m, err := flags.ToRESTMapper()
			if err != nil {
				return err
			}
			gvr, err := m.ResourceFor(schema.ParseGroupResource(typ).WithVersion(""))
			if err != nil || gvr.Group != denv1.GroupName || !slices.Contains([]string{"dens", "surfaces", "spaces"}, gvr.Resource) {
				return fmt.Errorf("@ needs a type before it: den, surface or space")
			}
			v := den
			if gvr.Resource != "dens" {
				s, err := surfaceOf(cmd.Context(), ns, den)
				if err != nil {
					return err
				}
				if s == nil {
					return fmt.Errorf("no Surface shows den %s/%s", ns, den)
				}
				v = s.Name
				if gvr.Resource == "spaces" {
					v = s.Spec.Space
				}
			}
			if slash {
				v = typ + "/" + v
			}
			args[i] = v
		}
		return nil
	}
	pre := root.PersistentPreRunE
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == cobra.ShellCompRequestCmd {
			os.Setenv("PATH", "")
		} else if err := self(cmd, args); err != nil {
			return err
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
	root.PersistentFlags().StringVar(&o.StateDir, "state-dir", stateDir, "directory for the token, serving certificate, kubeconfig and etcd data")
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
	run.Flags().StringVar(&o.Den, "den", "den", "den binary to start for each Den without a live Lease (empty: start none)")
	run.Flags().DurationVar(&o.UndoGrace, "undo-grace", time.Minute, "how long a deleted Space, Surface or Den can be brought back by creating it again")
	run.Flags().StringVar(&o.TCPAddr, "tcp", "", "also serve TLS on this address (e.g. 127.0.0.1:0), for stock kubectl and remote clients")
	srv := &cobra.Command{Use: "server", Short: "Manage the dennet API server"}
	srv.AddCommand(run)
	root.AddCommand(srv)

	var color bool
	logs := &cobra.Command{
		Use:   "logs NAME",
		Short: "Print a den's scrollback and screen as text",
		Long:  "Print a den's scrollback and screen as text, as den logs does; the alternate screen comes last.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, _, err := flags.ToRawKubeConfigLoader().Namespace()
			if err != nil {
				return err
			}
			c, err := denClient()
			if err != nil {
				return err
			}
			body, err := c.Get().Namespace(ns).Resource("dens").Name(args[0]).SubResource("log").
				Param("color", strconv.FormatBool(color)).Stream(cmd.Context())
			if err != nil {
				return err
			}
			defer body.Close()
			_, err = io.Copy(os.Stdout, body)
			return err
		},
	}
	logs.Flags().BoolVar(&color, "color", false, "keep colors")
	// kubectl's logs only knows pods. Overwrite it in place: kubectl's help
	// groups hold its pointer, so it keeps its spot there.
	if c, _, err := root.Find([]string{"logs"}); err == nil && c != root {
		root.RemoveCommand(c)
		*c, logs = *logs, c
	}
	root.AddCommand(logs)

	// get -o url prints each object's API URL, e.g.
	// http://dennet/apis/den.mkm.pub/v1alpha1/namespaces/default/dens/example.
	// kubectl's printers can't: no object field holds the plural resource.
	// get -o space prints each object's Space name, as -o name prints its
	// name: a Space's own, a Surface's spec.space, and for a Den the Space of
	// the Surface showing it.
	if get, _, err := root.Find([]string{"get"}); err == nil && get != root {
		run := get.Run
		get.Run = func(cmd *cobra.Command, args []string) {
			out := cmd.Flag("output").Value.String()
			if out != "url" && out != "space" {
				run(cmd, args)
				return
			}
			ns, enforce, err := flags.ToRawKubeConfigLoader().Namespace()
			cmdutil.CheckErr(err)
			all, _ := cmd.Flags().GetBool("all-namespaces")
			sel, _ := cmd.Flags().GetString("selector")
			files, _ := cmd.Flags().GetStringSlice("filename")
			recursive, _ := cmd.Flags().GetBool("recursive")
			kustomize, _ := cmd.Flags().GetString("kustomize")
			infos, err := cmdutil.NewFactory(flags).NewBuilder().Unstructured().
				NamespaceParam(ns).DefaultNamespace().AllNamespaces(all).
				FilenameParam(enforce, &resource.FilenameOptions{Filenames: files, Recursive: recursive, Kustomize: kustomize}).
				LabelSelectorParam(sel).ResourceTypeOrNameArgs(true, args...).
				ContinueOnError().Latest().Flatten().Do().Infos()
			for _, i := range infos {
				if out == "url" {
					fmt.Fprintln(cmd.OutOrStdout(), i.Client.Get().NamespaceIfScoped(i.Namespace, i.Namespaced()).
						Resource(i.Mapping.Resource.Resource).Name(i.Name).URL())
					continue
				}
				space := ""
				switch r := i.Mapping.Resource; {
				case r.Group != denv1.GroupName:
					cmdutil.CheckErr(fmt.Errorf("%s %s has no Space", r.Resource, i.Name))
				case r.Resource == "spaces":
					space = i.Name
				case r.Resource == "surfaces":
					space, _, _ = unstructured.NestedString(i.Object.(*unstructured.Unstructured).Object, "spec", "space")
				case r.Resource == "dens":
					s, err := surfaceOf(cmd.Context(), i.Namespace, i.Name)
					cmdutil.CheckErr(err)
					if s == nil {
						cmdutil.CheckErr(fmt.Errorf("no Surface shows den %s/%s", i.Namespace, i.Name))
					}
					space = s.Spec.Space
				default:
					cmdutil.CheckErr(fmt.Errorf("%s %s has no Space", r.Resource, i.Name))
				}
				fmt.Fprintln(cmd.OutOrStdout(), space)
			}
			cmdutil.CheckErr(err)
		}
	}

	// As kubectl's main: adds -v and the other klog flags; CheckErr prints errors.
	cmdutil.CheckErr(cli.RunNoErrOutput(root))
}
