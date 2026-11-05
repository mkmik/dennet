+++
title = "Lease"
summary = "coordination.k8s.io/v1 Lease, one per Den, renewed by den: liveness without status churn"
kind = "code"
intent = "induced"
research = "done"
status = "done"
milestone = "m1"
refs = ["internal/registry/lease.go"]

[[needs]]
on = "/core/apiserver"
why = "Served from the embedded API server"
+++

Leases stay in memory whatever storage wins: they're rewritten every few
seconds and useless after a restart.
