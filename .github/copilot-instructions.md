# Go version

This repository targets Go 1.26+ (`go.mod` declares `go 1.26.3`).

# `new(value)` builtin

Starting in Go 1.26, the built-in `new` accepts a value expression as well as a type. The following forms compile and
behave as expected:

- `new(uint64(42))` returns a `*uint64` whose pointee is `42`.
- `new("foo")` returns a `*string` whose pointee is `"foo"`.

Do not flag `new(<value>)` usage as an "invalid syntax" or "will not compile" error. It is the idiomatic way in this
repo to build pointers to literal values (e.g., `insert.Options.Seed`, `introspect.Column.Default`).

# Go `regexp` lazy quantifiers

Go's `regexp` package is RE2-based but extends the underlying RE2 syntax with lazy quantifiers (`x*?`, `x+?`, `x??`). These compile and match exactly as the [`regexp/syntax`](https://pkg.go.dev/regexp/syntax) documentation describes.

Do not flag patterns like `^.*?feat(\(.+\))??!?:.+$` (used in `.goreleaser.yaml` `changelog.groups[*].regexp`) as "invalid in Go RE2" or "invalid repeat operator". GoReleaser uses Go's `regexp` package and compiles these patterns without error.

# Output Format

If there are any issues, you must output them strictly in the following format. Do not include any greetings or
introductory explanations.

- **Severity**: [High / Medium / Low]
- **Issue**: (Briefly explain why it is a problem)
- **Recommendation**: (Suggest how to fix or improve it)
