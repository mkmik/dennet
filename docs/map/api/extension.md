+++
title = "Extension"
summary = "A program that shows its own UI in dense, in Surfaces of kind ui, served on spec.endpoint"
aka = ["extensions"]
kind = "code"
intent = "goal"
research = "done"
status = "done"
refs = ["pkg/apis/den/v1alpha1/types.go", "internal/registry/extension.go"]

[[needs]]
on = "/core/apiserver"
why = "Served from the embedded API server"
+++

From the dense PRD "A2UI surfaces in Dense". spec.endpoint is the absolute
path of the Unix socket the program listens on; clients (dense) connect
there and speak A2UI 1.0. status (phase Connected or Disconnected, and
since) is only written by dennet, on extensions/status (see
/controllers/extensions). The PRD has the program renew a lease in status;
dennet instead keeps a connection open, as for den Leases, so a running
extension costs no writes.
