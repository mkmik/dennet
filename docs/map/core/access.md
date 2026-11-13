+++
title = "Listeners and access"
summary = "Unix socket and TLS listeners, bearer tokens, and a small built-in policy"
kind = "code"
intent = "induced"
research = "done"
refs = ["internal/server/server.go"]

[[feature]]
id = "unix"
title = "Unix socket, mode 0600, for dens and local clients"
status = "done"
milestone = "m1"

[[feature]]
id = "tls"
title = "Optional TCP listener with TLS, for remote clients"
status = "done"

[[feature]]
id = "user-token"
title = "A user token from a file, which can do everything"
status = "done"
note = "Only on TCP; the Unix socket trusts every caller"

[[feature]]
id = "den-tokens"
title = "One token per den"
status = "todo"
milestone = "m5"

[[feature]]
id = "policy"
title = "A den token may only read its own Den, and patch its status"
status = "todo"
milestone = "m5"
needs = ["#den-tokens"]
+++

A small built-in policy, not full RBAC.
