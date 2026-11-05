+++
title = "Generated code"
summary = "Deepcopy and OpenAPI for the API types, from go generate"
kind = "code"
intent = "induced"
research = "done"
status = "done"
milestone = "m1"
refs = ["pkg/apis/den/v1alpha1/doc.go", "pkg/generated/openapi"]

[[needs]]
on = "/deps/k8s"
why = "deepcopy-gen and openapi-gen"
+++
