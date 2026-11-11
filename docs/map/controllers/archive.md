+++
title = "Archive"
summary = "spec.archived on a Space: delete its Surfaces and Dens, keep the Space to reopen"
kind = "code"
intent = "goal"
research = "done"
status = "done"
refs = ["internal/server/archive.go", "internal/registry/space.go"]

[[needs]]
on = "/api/space"
why = "The flag and status.archivedAt live on the Space"

[[needs]]
on = "/api/surface"
why = "Archiving deletes the Space's Surfaces"

[[needs]]
on = "/controllers/finalizer"
why = "Deleting a Den is how its den gets stopped"
+++

Archiving is how a person keeps a limit on how much is running. Spaces
they are not using get archived. Archived Spaces cost no processes and no
memory, and they stay listed so they can be reopened later.

The Space's `spec.archived: true` asks for it. It is a field on Space and
not a separate kind, so:
- reopening is one merge patch, `{"spec":{"archived":false}}`;
- the Space keeps its name, so child Spaces' `spec.parent` stays valid;
- a watch sees the Space change, not disappear.

When dennet sees the flag set, it deletes every Surface whose `spec.space`
is that Space, and every Den those Surfaces point at. The Space keeps
title, workingDir, parent, order, badges and tags. No layout is kept:
reopening starts with one new tab in `spec.workingDir`, as for a new
Space. dennet does the teardown itself, not the client, so the limit
holds whichever client set the flag. The same code would also delete
the Surfaces and Dens of a deleted Space, which nothing does today.

Archiving a Space sets the flag on every Space under it too, so their
Dens stop as well, and each can be reopened on its own.

dennet sets `status.archivedAt` when the teardown is done. A reopen list
sorts by it, newest first.

Only dennet writes status, on spaces/status; reopening clears
`status.archivedAt`. A Surface stays until its Den's delete is accepted, so
`archivedAt` means every Den was asked to stop, not that every den has
exited.

Clients don't show archived Spaces in the sidebar (dense, ccwt's
workspace list). A server-side filter would save each client doing it.
