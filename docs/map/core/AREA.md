+++
title = "Server"
band = "core"
summary = "The embedded API server, its storage, listeners and generated code"
+++

`internal/server/` and `pkg/generated/`, plus etcd embedded as storage. The
pattern is Tilt's: one binary with k8s.io/apiserver and its own types.
