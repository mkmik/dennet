+++
title = "Host"
summary = "Cluster-scoped: another machine running dennet, reached over ssh; named after its namespace"
kind = "code"
intent = "induced"
research = "done"
refs = ["pkg/apis/den/v1alpha1/types.go", "internal/registry/host.go"]

[[feature]]
id = "ssh"
title = "spec.ssh, the ssh destination, and spec.socketPath, the dennet socket there"
status = "done"

[[feature]]
id = "den-path"
title = "spec.denPath, den there, for clients that run it on the Host (dense attaching); else a login shell's PATH"
status = "done"

[[feature]]
id = "status"
title = "Capacity and last seen, from the PRD"
status = "todo"

[[needs]]
on = "/core/apiserver"
why = "Served from the embedded API server"
+++

Proposed in the PRD. A Host named `default` is refused: that is this
host's own namespace. `spec.hostName` on a Den is the option B idea and
is unrelated so far.
