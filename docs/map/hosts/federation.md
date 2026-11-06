+++
title = "Federation"
summary = "Each Host's default namespace shows up here as the namespace named after it"
kind = "code"
intent = "goal"
research = "outline"
refs = ["internal/server/remote.go"]

[[feature]]
id = "forward"
title = "Requests in namespace N go to Host N's default namespace; metadata.namespace is rewritten both ways"
status = "done"

[[feature]]
id = "ssh"
title = "One ssh per Host running dennet server dial-stdio, carrying HTTP/2 without TLS"
status = "done"

[[feature]]
id = "all-namespaces"
title = "Lists and watches across all namespaces (-A) include every Host"
status = "todo"
note = "Needs a fan-out and a combined resourceVersion"

[[feature]]
id = "upgrade"
title = "Forward upgraded connections (attach) to a Host"
status = "todo"
note = "Go's HTTP/2 client can't upgrade; attach needs its own stream"
needs = ["/proxy/subresources#attach"]

[[needs]]
on = "/api/host"
why = "A Host names the machine and how to reach it"

[[needs]]
fuzzy = "what a laptop dennet shows while a Host is unreachable"
why = "An open question in the PRD; today requests fail with 502"
+++

With option A each host has its own dennet. Nothing is synced: every
request is forwarded live, so the remote dennet keeps the only copy of its
objects. Only JSON bodies are rewritten; protobuf is asked for as JSON.
