+++
title = "Space"
summary = "A node in dense's sidebar tree: parent, order, and how its row looks"
aka = ["spaces"]
kind = "code"
intent = "goal"
research = "done"
status = "done"
refs = ["pkg/apis/den/v1alpha1/types.go", "internal/registry/space.go"]

[[needs]]
on = "/core/apiserver"
why = "Served from the embedded API server"

[[needs]]
on = "/core/codegen"
why = "Deepcopy and OpenAPI for the types"
+++

From the dense PRD. Outside tools (ccwt) create Spaces and keep title,
badges and state current; dense writes order, collapsed and renames.
spec.tags is a free-form list of strings, non-empty and unique.
spec.gather names other Spaces that clients also show under this one, in
list order after its own children, so one Space can show up in two
places: at home under its parent (a worktree under its repo) and under a
Space that gathers it (a piece of work spanning several repos). The
gathering Space owns the list, so the gathered Space does not know about
it. Names are unique, valid and not the Space's own; a name that matches
no Space shows nothing. Clients show gathered Spaces one level deep, so
two Spaces gathering each other is harmless. Archiving does not follow
gather: the gathered Space's home decides.
spec.workingDir, an absolute path, is where dens for the space's surfaces start.
While the space has one terminal, dennet keeps it at that terminal's directory
(see /controllers/follow-cwd).
spec.archived and status.archivedAt are for archiving (see
/controllers/archive); status is only written on spaces/status.
Nothing in it is tied to git. Validation only checks state, gather and that a
space is not its own parent; longer cycles are not caught.
