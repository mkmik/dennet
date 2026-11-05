+++
title = "Namespace"
summary = "v1 Namespace; default always exists, and writes create their namespace"
kind = "code"
intent = "induced"
research = "done"
status = "done"
refs = ["internal/registry/namespace.go"]

[[needs]]
on = "/core/apiserver"
why = "Served from the embedded API server"
+++

Not in the PRD. Dens and Leases are namespaced, and kubectl expects
namespaces to exist. No lifecycle: deleting a namespace leaves its objects
in place.
