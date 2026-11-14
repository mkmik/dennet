+++
title = "Liveness"
summary = "Sets status.phase Lost when a Den's den socket closes"
kind = "code"
intent = "induced"
research = "done"
status = "todo"
milestone = "m3"

[[needs]]
on = "/api/lease"
why = "What it watches"

+++

If the den comes back, den's next status write restores the phase.
