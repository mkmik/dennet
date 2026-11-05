+++
title = "Storage"
summary = "In-memory storage.Interface, with etcd-like global resource versions"
aka = ["memstore"]
kind = "code"
intent = "induced"
research = "done"
refs = ["internal/memstore/memstore.go"]

[[feature]]
id = "crud"
title = "Get, list, create, update, delete"
status = "done"
milestone = "m1"

[[feature]]
id = "watch"
title = "Watch, with 410 Gone past the event history"
status = "done"
milestone = "m1"

[[feature]]
id = "paging"
title = "Paging on list requests"
status = "todo"
note = "Lists always come back whole"
+++

Built behind the standard storage interface so the choice can change
without touching the types or controllers. On restart all objects are
gone; dens keep running and must re-register.
