+++
title = "Tests"
summary = "End-to-end tests over the Unix socket"
kind = "infra"
intent = "induced"
research = "done"
refs = ["internal/server/server_test.go"]

[[feature]]
id = "protocol"
title = "Plain HTTP the way den talks: create, single-object watch, status PATCH, 409, 410"
status = "done"
milestone = "m1"

[[feature]]
id = "manifests"
title = "The manifests directory"
status = "done"

[[feature]]
id = "real-den"
title = "A real den GETs its spec, watches it, and PATCHes status"
status = "todo"
milestone = "m2"
needs = ["/deps/den#spec-get", "/deps/den#watch", "/deps/den#status"]
+++
