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
go install mkm.pub/dennet@latest
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

## Remote hosts

A `Host` is another machine running dennet. Its objects show up here in the
namespace with the Host's name; on that machine they are in `default`.

```sh
dennet apply -f - <<EOF
apiVersion: den.mkm.pub/v1alpha1
kind: Host
metadata: {name: box}
spec:
  ssh: me@box.example  # anything ssh accepts, e.g. an alias from ~/.ssh/config
  # socketPath: /run/user/1000/dennet/dennet.sock  # optional, found by default
  # denPath: /home/me/bin/den  # optional, for clients that run den there
EOF
dennet get dens -n box
```

dennet runs `ssh -W <socket> <spec.ssh>`, so ssh forwards to the dennet socket
on that machine; dennet need not be on its PATH. Without `spec.socketPath`, a
shell there finds the socket first: `$XDG_RUNTIME_DIR/dennet/dennet.sock`, else
`$XDG_STATE_HOME/dennet/dennet.sock` (`~/.local/state/dennet/dennet.sock`).
One ssh carries every request for the Host. ssh runs in batch mode, so it
needs a key or an agent, not a password, and sshd must allow stream local
forwarding (the default).

A request in namespace `box` is sent there with `default` in its place, and
`metadata.namespace` is changed back to `box` in the answer. Nothing is
copied, so while the Host can't be reached its namespace answers 502.
`-A` lists only this machine's objects.

## Storage

dennet runs etcd inside its own process. Objects are kept in
`<state-dir>/etcd`, so they are still there after dennet restarts. To start
from nothing, stop dennet and delete that directory.

This etcd listens only on the Unix socket `<state-dir>/etcd.sock`. The state
dir has mode 0700, so only your user can reach it.

Old versions are compacted every 5 minutes. A watch that resumes from a
`resourceVersion` older than that gets `410 Gone` and has to list again.

## Develop

```sh
go generate ./...   # deepcopy and OpenAPI code
go test ./...
```

`docs/map/` is a [todography](https://github.com/mkmik/todography) map of
dennet's parts: what each does, how far along it is, and what it needs.
`todography next --all` lists the work that's left; `todography open` draws
the map. Change the map in the same commit as the code, and run
`todography lint` before committing.
