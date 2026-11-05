+++
title = "Host"
summary = "Cluster-scoped: a machine that can run dens, how to reach it, capacity, last seen"
kind = "code"
intent = "aspiration"
research = "deferred"

[[needs]]
on = "/core/apiserver"
why = "Served from the embedded API server"
+++

Proposed in the PRD. Open question: is it needed in v1, or only once there
is more than one host? `spec.hostName` already names one.
