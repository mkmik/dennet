+++
title = "Surface"
summary = "A tab of a Space, or its part of a box: a Den's terminal, a web page, or an Extension's panel"
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
id = "web"
title = "Kind web: a tab that shows one page, or a browser with navigation"
status = "done"
note = "spec.web has url or html, and navigation; dennet checks only that exactly one is set and the url is absolute"

[[feature]]
id = "glue"
title = "Glue a Surface beside another of its Space, for dense's combo tabs"
status = "done"
note = "spec.beside names the Surface it sits right of; dennet checks only that it isn't itself"

[[feature]]
id = "ui"
title = "Kind ui: a tab an Extension draws with A2UI"
status = "done"
note = "spec.ui.extension names the Extension; its A2UI surfaceId is the Surface's name"

[[feature]]
id = "box"
title = "Placement names a box: a slot of the client's window that shows the selected Space's Surfaces placed there"
status = "done"
note = "Any kind; any DNS label; spec.order orders a Space's Surfaces in a box. dennet refuses one glued beside another"

[[needs]]
on = "/api/space"
why = "Every Surface belongs to a Space"

[[needs]]
on = "/api/den"
why = "A terminal Surface attaches to a Den"
+++

From the dense PRD. Each kind has its own block next to kind: terminal
names a Den, web names a page (a URL, or the HTML itself). A web tab
shows only that page, or with navigation it is a small browser. An
agent can show an artifact with an html web tab glued beside its
terminal, and change the html to update it. Later kinds (diff, notes)
add their own blocks.

A Surface can be glued beside another Surface of its Space: dense then
shows the two as one combo tab, side by side, with fused handles (dense's
t8y:/tabs/combo). The bond is spec.beside on the glued Surface, kept here like
order and title, so every client and window sees the same combos. A bond
naming a Surface that has gone counts as none, so deleting a Surface needs
no cleanup. Not called attach: that is den's attach subresource.

spec.placement says where a Surface goes: a tab when empty, or else the
name of a box. A box is a slot of the client's window with a size the
client sets, not its content: it shows the selected Space's Surfaces placed
there, so switching Spaces changes what is in it, never where it is or how
big. dennet keeps only the name. Each client decides where its boxes go and
how tall they are, and gives a box to every name a Surface uses, set up or
not, so a name from an Extension or another machine never hides a Surface.
It replaces the PRD's UISurface resource and its slots: a tab is the PRD's
tab slot, a tab glued beside an agent's terminal its sidePane, and a box
its sidebar slot, for any kind, not just ui. A first try, placement
sidebar, stacked each Space's Surfaces under the Space tree as tall as they
drew, and moved the tree on every switch; it is gone.
