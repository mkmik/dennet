+++
title = "Finalizer"
summary = "Adds den.mkm.pub/den on create; removes it once dennet has shut the den down"
kind = "code"
intent = "induced"
research = "done"
status = "done"
milestone = "m3"
refs = ["internal/registry/den.go", "internal/server/dens.go"]

[[needs]]
on = "/deps/den#socket"
why = "dennet stops the den with a shutdown frame on its control socket"

[[needs]]
on = "/api/lease"
why = "A den that won't stop is let go once its Lease has expired"
+++

Deleting the Den is the kill request. Because of the finalizer, the Den
stays while dennet sends its den shutdown over the den's socket. den stops
the program (SIGHUP, then SIGKILL after 5 s), writes a final status and
exits. Then dennet removes the finalizer and deletes the den's Lease (dennet
has no garbage collector). No den listening counts as stopped. den itself
doesn't need to watch for a deletionTimestamp.
