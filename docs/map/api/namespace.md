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
namespaces to exist. Registering a Host creates the namespace named after it,
owned by the Host, and deleting the Host deletes it. A namespace that
already existed gets no owner and stays.
No lifecycle: deleting a namespace leaves its objects
in place.
