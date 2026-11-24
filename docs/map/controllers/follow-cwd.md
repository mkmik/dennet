+++
title = "Follow cwd"
summary = "A Space with one terminal takes that terminal's directory as its spec.workingDir"
kind = "code"
intent = "goal"
research = "done"
status = "done"
refs = ["internal/server/followcwd.go"]

[[needs]]
on = "/api/space"
why = "It writes the Space's spec.workingDir"

[[needs]]
on = "/api/surface"
why = "A Space's terminal Surfaces say which Den to follow"

[[needs]]
on = "/api/den"
why = "den reports the shell's directory in status.cwd"
+++

A new Space usually starts in the home directory. The person then `cd`s
somewhere, makes a repo, and works there. New tabs of that Space should
start there too, without setting `spec.workingDir` by hand.

So while a Space has exactly one terminal Surface, dennet sets the
Space's `spec.workingDir` to that terminal's Den's `status.cwd` whenever
the two differ. With two or more terminals there is no single right
directory, so dennet leaves the Space alone; when only one is left it
follows that one again. Web and UI tabs don't count. Archived Spaces are
left alone.

den sets `status.cwd` from the directory the shell reports (OSC 7), even
when no client is attached. So a `cd` made from another machine, or in a
tab dense has detached, is still seen. A shell that doesn't report its
directory (a plain ssh session, say) leaves `status.cwd` empty, and the
Space keeps its directory.

It runs on whichever dennet holds the Space; a Host's dennet runs its own.
It writes spec, not status, so a person or tool can still set
`spec.workingDir` themselves; it holds until the one terminal changes
directory.
