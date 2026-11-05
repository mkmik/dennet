+++
title = "den"
summary = "One process per session, holding the PTY and history; a separate Zig project"
kind = "external"
intent = "induced"
research = "outline"
refs = ["https://github.com/mkmik/den"]

[[feature]]
id = "socket"
title = "Control socket: attach, input, signal, and screen and scrollback as text"
status = "done"

[[feature]]
id = "spec-get"
title = "GET its spec over HTTP or http+unix"
status = "done"
milestone = "m2"
note = "Today as den apply -f URL; the den PRD has den --spec URL"

[[feature]]
id = "watch"
title = "Single-object watch of its Den; 410 Gone re-GETs"
status = "todo"
milestone = "m2"

[[feature]]
id = "status"
title = "--status URL: merge PATCH dens/status, conditional on metadata.uid"
status = "todo"
milestone = "m2"

[[feature]]
id = "lease"
title = "Create and renew its Lease, owned by the Den"
status = "todo"
milestone = "m3"

[[feature]]
id = "deletion"
title = "Treat a deletionTimestamp like DELETED: stop, write a final status"
status = "todo"
milestone = "m3"
note = "The dennet PRD asks the den PRD for this"

[[feature]]
id = "token-file"
title = "--token-file, re-read on each request"
status = "todo"
milestone = "m5"

[[feature]]
id = "history-ranges"
title = "history requests for a range, served from the on-disk log"
status = "todo"
note = "den's milestone 2"
+++

Only what dennet needs from den. den's own milestones are in the den PRD;
its HTTP backend (GET, watch, status PATCH, Lease) is its milestone 4.
