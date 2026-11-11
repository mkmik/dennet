+++
title = "dense"
summary = "The flagship client, a fork of the Ghostty macOS app"
aka = ["dance"]
kind = "external"
intent = "goal"
research = "deferred"

[[needs]]
on = "/api/den"
why = "Lists, creates and watches dens"

[[needs]]
on = "/api/space"
why = "Draws the sidebar tree from Spaces"

[[needs]]
on = "/api/surface"
why = "Draws each space's tabs from Surfaces"

[[needs]]
on = "/controllers/archive"
why = "Hides archived Spaces and offers to reopen them"

[[needs]]
on = "/proxy/subresources#attach"
why = "Its terminal is an attached den"

[[needs]]
on = "/hosts/federation#ssh"
why = "Remote reach in v1, so dennet needs no network auth at first"
+++

A separate repository; its own progress isn't tracked here. The den PRD
calls it dance.

The PRD's goal: give clients one place to list, create, watch and attach
to dens, local or remote.
