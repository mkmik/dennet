+++
title = "Surface"
summary = "One tab inside a Space; kind terminal points at a Den"
aka = ["surfaces"]
kind = "code"
intent = "goal"
research = "done"
status = "done"
refs = ["pkg/apis/den/v1alpha1/types.go", "internal/registry/space.go"]

[[needs]]
on = "/api/space"
why = "Every Surface belongs to a Space"

[[needs]]
on = "/api/den"
why = "A terminal Surface attaches to a Den"
+++

From the dense PRD. Only kind terminal exists; later kinds (browser,
diff, notes) each add their own block next to terminal.
