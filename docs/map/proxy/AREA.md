+++
title = "Subresource proxy"
band = "logic"
summary = "Imperative calls on a Den, forwarded to the den's Unix socket"
+++

dennet keeps no terminal state. Each subresource call is proxied to the
den's socket and translated to den's frame protocol, so terminal bytes
never touch storage.
