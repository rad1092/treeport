# Contributing

Treeport is a read-only whole-tree filename portability preflight. These rules
also guide AI coding agents working in this repository.

- Never rename, copy, or extract the input.
- Keep the public Go API and the JSON schema documented in
  [docs/contract.md](docs/contract.md). New fields are additive; `status`
  values and issue codes do not change meaning.
- Treat profile results as model-scoped, not universal filesystem or security
  guarantees. Hints may explain what a destination most likely does, but the
  model decides `status`.
- Preserve original name bytes in base64.
- `testdata/corpus.json` was written before the implementation from destination
  rules. Add cases; do not edit expectations to match new code.
- Large stress inputs are generated in temporary directories and removed;
  commit compact evidence only.

Before a release:

```sh
go test ./...
go test -race ./...
go vet ./...
```
