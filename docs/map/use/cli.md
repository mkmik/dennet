+++
title = "dennet CLI"
summary = "One binary: runs the server, and is a kubectl that talks to it over the Unix socket"
aka = ["dennet server run", "main.go"]
kind = "code"
intent = "goal"
research = "done"
refs = ["main.go"]

[[feature]]
id = "server-run"
title = "dennet server run"
status = "done"
milestone = "m1"

[[feature]]
id = "kubectl"
title = "Every kubectl command, over the Unix socket, with no kubeconfig"
status = "done"
needs = ["/core/access#unix"]

[[feature]]
id = "no-plugins"
title = "Hide kubectl-* plugins from help and completion"
status = "done"

[[feature]]
id = "klog-flags"
title = "kubectl's -v and klog flags"
status = "done"

[[feature]]
id = "self"
title = "@ names the den a command runs in: den @, surface @, space @, logs @"
status = "done"
note = "Read from $DEN_SOCKET, as wezterm cli reads $WEZTERM_PANE"

[[feature]]
id = "output-space"
title = "get -o space prints the Space of each Den, Surface or Space"
status = "done"

[[needs]]
on = "/core/apiserver"
why = "dennet server run starts it"

[[needs]]
on = "/deps/kubectl"
why = "The kubectl half is kubectl's own command tree"
+++

`dennet` ignores `~/.kube/config` and `$KUBECONFIG` and always dials the
dennet socket. Not in the PRD; added so the binary is its own client.

Inside a den, `@` stands for that den: `dennet get den @ -o space` prints
the Space it is shown in. `surface @` is the Surface that shows the den, and
`space @` is that Surface's Space. The namespace comes from `$DEN_SOCKET` too.
