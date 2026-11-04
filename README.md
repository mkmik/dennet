# dennet

dennet is one Go binary that serves a Kubernetes-style API for den
sessions. It defines `Den` in
`den.mkm.pub/v1alpha1`, and also serves `coordination.k8s.io/v1` `Lease`,
which dens renew to show they are alive. `kubectl` and client-go work with it
unchanged.

It also serves `v1` `Namespace`. The `default` namespace always exists, and
creating any object in a namespace creates that namespace if needed. Deleting a
namespace does not delete the objects in it.

`dens/log` is a subresource that returns the same text as `den logs`: the
scrollback and the screen (`?color=true` keeps colors). dennet gets it from the
den's control socket. That is the path in `status.socket`, or, if that is
empty, the path den itself uses for that namespace and name on this host.
`dennet logs NAME [--color]` reads it.

## Run

```sh
go install .
dennet server run
dennet get dens
```

`dennet` is also a full kubectl: every kubectl command works, and it talks to
the server over the Unix socket, so it needs no kubeconfig, token or TLS. It
ignores `~/.kube/config` and `$KUBECONFIG`.

Stock `kubectl` works too, over TLS, if you ask for a TCP port:

```sh
dennet server run --tcp=127.0.0.1:0
KUBECONFIG=~/.local/state/dennet/kubeconfig kubectl get dens
```

dennet listens on:

- A Unix socket (mode 0600, no token needed). dens and local clients use it.
  It is `$XDG_RUNTIME_DIR/dennet/dennet.sock`, or
  `~/.local/state/dennet/dennet.sock` when `XDG_RUNTIME_DIR` is not set
  (as on macOS).
- A TLS port, only with `--tcp` (`127.0.0.1:0` picks a free port). It needs
  the token in `~/.local/state/dennet/token`. dennet writes a matching
  kubeconfig on every start.

The token, certificate and kubeconfig live in `$XDG_STATE_HOME/dennet`
(default `~/.local/state/dennet`). Use `--state-dir` and `--socket` to change
where they go; the client commands take the same flags.

Everything is kept in memory. When dennet restarts, all objects are gone. A
watch that resumes from an old `resourceVersion` gets `410 Gone`.

## Develop

```sh
go generate ./...   # deepcopy and OpenAPI code
go test ./...
```
