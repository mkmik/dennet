+++
title = "Surface"
summary = "One tab inside a Space; kind terminal points at a Den"
aka = ["surfaces"]
kind = "code"
intent = "goal"
research = "done"
refs = ["pkg/apis/den/v1alpha1/types.go", "internal/registry/space.go"]

[[feature]]
id = "terminal"
title = "Kind terminal: a tab that attaches to a Den"
status = "done"

[[feature]]
id = "glue"
title = "Glue a Surface beside another of its Space, for dense's combo tabs"
status = "done"
note = "spec.beside names the Surface it sits right of; dennet checks only that it isn't itself"

[[needs]]
on = "/api/space"
why = "Every Surface belongs to a Space"

[[needs]]
on = "/api/den"
why = "A terminal Surface attaches to a Den"
+++

From the dense PRD. Only kind terminal exists; later kinds (browser,
diff, notes) each add their own block next to terminal.

A Surface can be glued beside another Surface of its Space: dense then
shows the two as one combo tab, side by side, with fused handles (dense's
t8y:/tabs/combo). The bond is spec.beside on the glued Surface, kept here like
order and title, so every client and window sees the same combos. A bond
naming a Surface that has gone counts as none, so deleting a Surface needs
no cleanup. Not called attach: that is den's attach subresource.
