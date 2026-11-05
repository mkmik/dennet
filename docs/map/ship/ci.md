+++
title = "CI"
summary = "gofmt, go vet, go test -race and todography lint on every push and pull request"
aka = ["depot"]
kind = "infra"
intent = "goal"
research = "done"
status = "done"
refs = [".depot/workflows/ci.yml"]

[[needs]]
on = "/ship/tests"
why = "The suite it runs"
+++
