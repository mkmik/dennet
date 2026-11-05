+++
title = "Finalizer"
summary = "Adds den.mkm.pub/den on create; removes it once den reports it has stopped"
kind = "code"
intent = "induced"
research = "done"
status = "todo"
milestone = "m3"

[[needs]]
on = "/deps/den#deletion"
why = "den must stop on a deletionTimestamp, not wait for DELETED"

[[needs]]
on = "/controllers/liveness"
why = "A Den whose Lease has expired is released after a timeout"
+++

Deleting the Den is the kill request. Because of the finalizer, den sees a
MODIFIED event with `metadata.deletionTimestamp`, stops the program,
writes a final status, and then the finalizer lets the object go.
