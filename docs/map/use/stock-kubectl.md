+++
title = "Stock kubectl"
summary = "kubectl, client-go and any watch-based tool, unmodified"
kind = "code"
intent = "goal"
research = "done"
status = "done"
milestone = "m1"

[[needs]]
on = "/core/access#tls"
why = "kubectl can't dial a Unix socket"

[[needs]]
on = "/api/den"
why = "What it lists, creates and watches"
+++

A PRD goal. `dennet server run --tcp=127.0.0.1:0`, then
`KUBECONFIG=~/.local/state/dennet/kubeconfig kubectl get dens`.
