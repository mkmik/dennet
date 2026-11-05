+++
title = "Liveness"
summary = "Watches Leases; sets status.phase Lost when a Den's Lease expires"
kind = "code"
intent = "induced"
research = "done"
status = "todo"
milestone = "m3"

[[needs]]
on = "/api/lease"
why = "What it watches"

[[needs]]
on = "/deps/den#lease"
why = "den has to renew a Lease for one to expire"
+++

If renewal resumes, den's next status write restores the phase.
