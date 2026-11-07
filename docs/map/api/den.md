+++
title = "Den"
summary = "One background terminal session; same spec and status as in the den PRD"
aka = ["dens", "den.mkm.pub"]
kind = "code"
intent = "goal"
research = "done"
refs = ["pkg/apis/den/v1alpha1/types.go", "internal/registry/den.go"]

[[feature]]
id = "crud"
title = "Create, update, delete; defaults; generation bumps; spec.command is immutable"
status = "done"
milestone = "m1"

[[feature]]
id = "status"
title = "dens/status, den only; a stale metadata.uid gets 409 Conflict"
status = "done"
milestone = "m1"
note = "Any client can write it until the den-scoped policy lands"

[[feature]]
id = "table"
title = "kubectl get dens shows phase, host and title"
status = "done"
milestone = "m1"

[[feature]]
id = "spawn"
title = "Creating a Den starts a den on its spec.hostName"
status = "done"
milestone = "m3"
needs = ["/hosts/spawner#spawn"]

[[feature]]
id = "finalizer"
title = "Deleting a Den waits for den to report it has stopped"
status = "done"
milestone = "m3"
needs = ["/controllers/finalizer"]

[[feature]]
id = "lost"
title = "Phase Lost when the den's Lease expires"
status = "todo"
milestone = "m3"
needs = ["/controllers/liveness"]

[[feature]]
id = "cleanup"
title = "Exited Dens go away after history.keepAfterExit"
status = "todo"
milestone = "m5"
needs = ["/controllers/cleanup"]

[[feature]]
id = "restart"
title = "restartPolicy OnFailure"
status = "todo"
milestone = "m5"
note = "The field is defaulted and validated; nothing acts on it yet"
needs = ["/controllers/restart"]

[[needs]]
on = "/core/apiserver"
why = "Served from the embedded API server"

[[needs]]
on = "/core/storage"
why = "Where Dens live"

[[needs]]
on = "/api/namespace"
why = "Dens are namespaced"

[[needs]]
on = "/core/codegen"
why = "Deepcopy and OpenAPI for the types"
+++

`spec` is written by whoever creates the Den; `status` only by den.
dennet's additions over the den PRD: `spec.hostName`,
`spec.restartPolicy`, and the `den.mkm.pub/den` finalizer.
