+++
title = "dennet logs"
summary = "Prints a den's scrollback and screen, like den logs"
kind = "code"
intent = "goal"
research = "done"
status = "done"
refs = ["main.go"]

[[needs]]
on = "/proxy/subresources#log"
why = "It reads dens/log"
+++

Replaces kubectl's `logs`, which only knows pods, in the same spot in
the help.
