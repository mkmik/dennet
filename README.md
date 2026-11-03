# dennet

dennet is one Go binary that serves a Kubernetes-style API for den
sessions. It defines `Den` in
`den.mkm.pub/v1alpha1`, and also serves `coordination.k8s.io/v1` `Lease`,
which dens renew to show they are alive. `kubectl` and client-go work with it
unchanged.

## Run

```sh
go run .
export KUBECONFIG=~/.local/state/dennet/kubeconfig
kubectl get dens
```

dennet listens in two places:

- A Unix socket (mode 0600, no token needed). dens and local clients use it.
  It is `$XDG_RUNTIME_DIR/dennet/dennet.sock`, or
  `~/.local/state/dennet/dennet.sock` when `XDG_RUNTIME_DIR` is not set
  (as on macOS).
- A TLS port on 127.0.0.1 (a random port by default; change it with `--tcp`,
  or turn it off with `--tcp=`). It needs the token in `~/.local/state/dennet/token`.
  dennet writes a matching kubeconfig on every start.

The token, certificate and kubeconfig live in `$XDG_STATE_HOME/dennet`
(default `~/.local/state/dennet`). Use `--state-dir` and `--socket` to change
where they go.

Everything is kept in memory. When dennet restarts, all objects are gone. A
watch that resumes from an old `resourceVersion` gets `410 Gone`.

## Develop

```sh
go generate ./...   # deepcopy and OpenAPI code
go test ./...
```
