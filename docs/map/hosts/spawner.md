+++
title = "Local spawner"
summary = "Option A: forks a den for each Den on this host, and finds them again after a restart"
kind = "code"
intent = "induced"
research = "done"
milestone = "m3"
refs = ["internal/server/spawn.go"]

[[feature]]
id = "spawn"
title = "Start den fully detached (setsid), with --spec and --status URLs at dennet's socket"
status = "done"
note = "den apply -f <Den URL> --unix-socket <dennet socket>, for each Den whose den socket isn't open"
needs = ["/deps/den#spec-get", "/deps/den#status"]

[[feature]]
id = "adopt"
title = "After a restart, find running dens by their sockets; never respawn one"
status = "done"
note = "dennet dials every den before starting any; den refuses to start twice"
needs = ["/deps/den#socket"]

[[feature]]
id = "pid-check"
title = "Confirm a den's UID over its socket before signalling a PID from status"
status = "todo"
note = "So a reused PID is never hit"

[[feature]]
id = "token"
title = "Pass each den its own token file"
status = "todo"
milestone = "m5"
needs = ["/core/access#den-tokens", "/deps/den#token-file"]
+++

Restarting dennet must never kill a session. Open question: confirm option
A, and when to add the host agent mode.
