+++
title = "Restart"
summary = "For restartPolicy OnFailure, replaces a failed Exited Den with a new one, with backoff"
kind = "code"
intent = "induced"
research = "done"
status = "todo"
milestone = "m5"

[[needs]]
on = "/deps/den#status"
why = "It acts on phase Exited with a non-zero exit code"
+++

The replacement has a new UID, so the old den can't write onto it.
Restarting the program on exit is dennet's decision, not den's.
