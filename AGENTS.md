# treeport
Read-only whole-tree filename portability preflight. Do not rename/copy/extract input.
Keep the public Go API and JSON schema documented. Treat profile results as model-scoped,
not universal filesystem or security guarantees. Preserve original name bytes in base64.
Run go test ./..., go test -race ./..., go vet ./... before release. Large stress inputs
must be generated in temporary directories and removed; commit compact evidence only.
