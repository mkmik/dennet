+++
title = "Den subresources"
summary = "snapshot, input, signal, attach and history on dens/<name>"
kind = "code"
intent = "goal"
research = "outline"
refs = ["internal/registry/denlog.go"]

[[feature]]
id = "log"
title = "dens/log: screen and scrollback as text (?color=true keeps colors)"
status = "done"
note = "Not in the PRD by this name; it is the text half of snapshot"

[[feature]]
id = "snapshot"
title = "dens/snapshot: current screens and recent scrollback, as text or VT"
status = "todo"
milestone = "m4"
note = "dens/log already serves the text form"

[[feature]]
id = "input"
title = "dens/input: send keys or bytes without attaching"
status = "todo"
milestone = "m4"

[[feature]]
id = "signal"
title = "dens/signal: send a signal to the program"
status = "todo"
milestone = "m4"

[[feature]]
id = "attach"
title = "dens/attach: snapshot, then live bytes, input both ways"
status = "todo"
milestone = "m4"
note = "An HTTP upgrade carrying den's frames unchanged. Open: WebSocket only, or a raw upgrade too"

[[feature]]
id = "history"
title = "dens/history: logged lines in a range, streamed"
status = "todo"
needs = ["/deps/den#history-ranges"]

[[needs]]
on = "/deps/den#socket"
why = "Every call becomes frames on the den's control socket"

[[needs]]
on = "/api/den"
why = "status.socket says where the den listens"
+++

For a den on the same host, dennet connects to `status.socket`. Dens on
other hosts depend on how spawning and hosts are settled.
