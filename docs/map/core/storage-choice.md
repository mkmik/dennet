+++
title = "Storage choice"
summary = "Persist objects in etcd embedded in dennet"
kind = "code"
intent = "aspiration"
research = "done"
status = "done"

[[needs]]
on = "/core/storage"
why = "The choice is carried out there"
+++

The PRD left this open: ephemeral, rebuilt from dens, or persistent.
Persistent won, with embedded etcd: it is the backend k8s.io/apiserver
already ships, so there is no storage code of our own. The in-memory store
and the manifests directory that refilled it after a restart are gone.

Considered and dropped: kine with SQLite, and a store that mirrors objects
to files that can be edited both on disk and through the API. A read and
write view of objects as files may come back later as a FUSE mount that
talks to the API (with macFUSE on macOS).
