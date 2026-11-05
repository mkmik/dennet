+++
title = "API server"
summary = "Embedded k8s.io/apiserver: scheme, API groups, OpenAPI"
kind = "code"
intent = "induced"
research = "done"
status = "done"
milestone = "m1"
refs = ["internal/server/server.go"]

[[needs]]
on = "/core/storage"
why = "Where every object lives"

[[needs]]
on = "/core/access"
why = "How clients reach it"

[[needs]]
on = "/deps/k8s"
why = "It is k8s.io/apiserver"
+++
