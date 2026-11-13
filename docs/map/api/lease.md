+++
title = "Lease"
summary = "coordination.k8s.io/v1 Lease, one per Den whose den's socket dennet holds open: liveness without writes"
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

Leases are not stored. dennet keeps a connection open to every local den's
control socket (and sends nothing on it); while it is open the Den has a
Lease, and when the den exits the kernel closes it and the Lease is gone
(DELETED on watches). renewTime is the time of each read, so the usual
"renewTime + leaseDurationSeconds > now" rule holds.

Leases are read-only (get, list, watch). Without the patch verb in
discovery, den doesn't renew; dens started before that get 405 until they
restart. Dens renewing every 10 s were almost all of an idle dennet's CPU
(etcd, watch fan-out, wakeups).

After a restart dennet dials every den before answering Lease reads, so no
Lease still means no den. A hung den with its socket open still counts as
alive, and dens on other machines aren't covered.
