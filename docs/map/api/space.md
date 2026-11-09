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
spec.workingDir, an absolute path, is where dens for the space's surfaces start.
Nothing in it is tied to git. Validation only checks state and that a
space is not its own parent; longer cycles are not caught.
