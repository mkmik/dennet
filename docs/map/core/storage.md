+++
title = "Storage"
summary = "etcd embedded in the dennet process, persisted in the state dir"
aka = ["etcd", "embedded etcd"]
kind = "code"
intent = "induced"
research = "done"
refs = ["internal/server/server.go"]

[[feature]]
id = "crud"
title = "Get, list, create, update, delete"
status = "done"
milestone = "m1"

[[feature]]
id = "watch"
title = "Watch, with 410 Gone once etcd compacts past the resourceVersion"
status = "done"
milestone = "m1"

[[feature]]
id = "persist"
title = "Objects survive a dennet restart"
status = "done"

[[feature]]
id = "paging"
title = "Paging on list requests"
status = "todo"
note = "Lists always come back whole"

[[needs]]
on = "/deps/k8s"
why = "k8s.io/apiserver's own etcd3 storage, the one kube-apiserver uses"
+++

etcd runs inside dennet, with its data in `<state-dir>/etcd`. It listens on
the Unix socket `<state-dir>/etcd.sock` and on no peer port. The API server
uses its stock etcd3 storage, without a watch cache, and compacts every 5
minutes.
