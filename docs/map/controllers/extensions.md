+++
title = "Extensions"
summary = "Extension status from a held connection to its endpoint; deletes what gone Extensions owned"
kind = "code"
intent = "goal"
research = "done"
status = "done"
refs = ["internal/server/extensions.go"]

[[needs]]
on = "/api/extension"
why = "It watches Extensions and writes their status"

[[needs]]
on = "/api/surface"
why = "It deletes Surfaces whose Extension is gone"
+++

dennet keeps a connection open to each local Extension's endpoint, as it
does to dens for Leases, and writes status.phase only when it changes. An
Extension Disconnected for 10 minutes is deleted. dennet has no general
garbage collector, so this controller also deletes the Surfaces whose
ownerReference names an Extension that is gone (checked with a live GET,
since the cache may lag a fresh Extension).
