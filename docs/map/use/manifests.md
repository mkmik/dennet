+++
title = "Manifests directory"
summary = "Applies the YAML and JSON files in a directory as they change"
aka = ["--manifests", "~/.config/dennet/manifests"]
kind = "code"
intent = "goal"
research = "done"
refs = ["internal/manifests/manifests.go"]

[[feature]]
id = "apply"
title = "Server-side apply of every file at start and on change"
status = "done"

[[feature]]
id = "prune"
title = "Delete objects removed from a file, or whose file is gone"
status = "done"
note = "An object listed in two files is deleted when either drops it"

[[feature]]
id = "watch"
title = "Find changes from the OS, without polling"
status = "done"
needs = ["/deps/fsnotify"]

[[needs]]
on = "/core/apiserver"
why = "It applies through the server's loopback client, once the API is up"
+++

Not in the PRD. Everything is in memory, so these files are how objects
come back after a restart.
