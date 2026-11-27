+++
title = "Undo"
summary = "A deleted Space, Surface or Den stays for a minute, and creating it again brings it back"
kind = "code"
intent = "goal"
research = "done"
status = "done"
refs = ["internal/registry/undo.go", "internal/server/undo.go"]

[[needs]]
on = "/api/space"
why = "Spaces can be brought back"

[[needs]]
on = "/api/surface"
why = "Surfaces can be brought back"

[[needs]]
on = "/api/den"
why = "Dens can be brought back, with their den still running"

[[needs]]
on = "/controllers/finalizer"
why = "A Den's den is only stopped once it can no longer be brought back"
+++

Any client can delete a Space and its tabs (dense, dowt, `dennet delete`),
and any client can then offer Undo. For that, deleting does not remove
anything at once.

Every Space, Surface and Den gets the finalizer `den.mkm.pub/undo`.
Deleting one sets its `deletionTimestamp`, and it stays. Clients hide
objects with a `deletionTimestamp`, so to the person it is gone. After
`--undo-grace` (1 minute by default), dennet removes the finalizer and the
object goes. A Den's den keeps running until then; only after that does
the `den.mkm.pub/den` finalizer stop it.

To undo, a client creates the object again with the same name. While the
old one is still there, dennet puts the new one in its place and keeps its
uid and status. The `deletionTimestamp` is cleared. A Den brought back
this way has the same den, still running, so its tabs attach to the same
shell. Nothing refers to objects by uid except den's own status writes,
which is why keeping the uid matters for Dens only. Once the finalizer is
gone, creating makes a new object, as before.

Deleting with `gracePeriodSeconds=0` skips the wait: dennet removes the
finalizer first. A client that restarts a Den (delete, then create) uses
that.

Objects made before this existed have no finalizer and go at once.
