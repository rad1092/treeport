# Check names before moving files

Treeport is useful to an archive builder, export-service developer or person
moving a tree between filesystems. It tells you which names conflict under a
chosen destination model. It does not rename files or extract archive contents.

Install the binary for your architecture from
[Releases](https://github.com/rad1092/treeport/releases/tag/v0.1.3) and verify its
archive against `SHA256SUMS`. Or use the pinned `go install` command in the README.
`treeport version` should print `treeport 0.1.3`.

## A passing report, then a useful failure

Create `names.jsonl` with one JSON object per line:

```jsonl
{"path":"reports/week-42.csv"}
```

Run (PowerShell and POSIX shells both accept this single-quoted Windows root):

```sh
treeport manifest --profile windows --root 'C:\export' --json names.jsonl
```

Expect exit `0`, `status: "known-compatible"` and `complete: true`. Now append:

```jsonl
{"path":"reports/CON.txt"}
{"path":"Reports/week-42.csv"}
```

Run again. Expect exit `1` and `status: "incompatible"`: a reserved Windows name
and case-converging parent/file names now need attention. Inspect `issues` and
the original paths before changing your export naming policy. The manifest is
read only; it is safe to model names your current filesystem cannot represent.

For actual data, use `scan` with a directory or `zip` with an existing ZIP. Keep
all flags **before** the input path. `--root` is the future destination root;
it does not need to exist on the computer running the check. Redirect JSON to a
report outside the source tree to avoid including the report itself in a scan.

## Troubleshooting

| Symptom | Next step |
| --- | --- |
| `treeport` not found after `go install` | Add `go env GOBIN` (or `$(go env GOPATH)/bin` when empty) to PATH, or invoke the binary by its full path. |
| `--root` rejected | For `windows`, use an absolute drive or UNC destination, not a relative directory. |
| Exit `3` | Read the uncertainty reasons. Unsupported filesystem semantics or symlinks are not certified as compatible. |
| Exit `2` | Correct arguments, input syntax, permissions or explicit resource limits; this is not an ordinary incompatibility finding. |
| `complete: false` | Treat it as an incomplete diagnosis; inspect the limit that truncated the report. |

On 2026-10-12, the public v0.1.3 macOS arm64 archive checksum was verified and
the installed CLI was exercised with passing/failing manifests, a real directory
and ZIP names on macOS 27.0.1. Source unit tests, race tests and `go vet` passed
with Go 1.27.1. Results remain scoped to the selected model, not universal
filesystem, mount or archive-safety guarantees.
