+++
title = "Host agent"
summary = "Option B: dennet host, an agent mode that spawns and proxies for a central dennet"
kind = "code"
intent = "aspiration"
research = "deferred"
status = "todo"

[[needs]]
on = "/hosts/spawner"
why = "It does the spawner's job on each host"

[[needs]]
fuzzy = "a connection from each host back to the server, like kubelet's"
why = "The central dennet has to reach dens through it"
+++
