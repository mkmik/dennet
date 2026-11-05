+++
title = "Storage choice"
summary = "Rebuild objects from running dens, or persist them (SQLite via kine, or a file store)"
kind = "code"
intent = "aspiration"
research = "deferred"
status = "todo"

[[needs]]
on = "/core/storage"
why = "It replaces or wraps the in-memory store"

[[needs]]
fuzzy = "how den tells a dennet restart from a deletion"
why = "Likely: a fresh GET returning 404 means re-create from the last spec; DELETED or a deletionTimestamp means stop"
+++

Deliberately undecided in the PRD: ephemeral, rebuilt from dens, or
persistent.
