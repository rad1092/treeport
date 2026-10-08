# Treeport

[한국어](README.ko.md) · [Profiles](docs/profiles.md) · [JSON/API contract](docs/contract.md) · [Measured comparison](docs/comparison.md)

**Check a whole filename tree before exporting, packaging, or moving it.** Treeport is a Go library and standalone CLI that finds names that converge on the same destination, explains why they converge, and includes the destination root in path budgets. It reads directory names, JSONL manifests, or ZIP entry names. It never renames, copies, or extracts files.

```text
CAFÉ/a?b.txt
café/a*b.txt
       ↓ export-fold: NFC → case fold → replace → trim → truncate
café/a_b.txt     collision: normalization + case + replacement
```

The two parents also converge, even when their children have different names. Reports include those implicit parent conflicts, duplicate entries, file/directory obstructions, and exact original bytes.

Use Treeport in export services, archive builders, artifact pipelines, and pre-commit checks. Choose a destination **model** explicitly: `posix`, `windows`, `macos`, or the precisely specified `export-fold` transformation pipeline. Results are `known-compatible`, `incompatible`, or `unknown`. The OS profiles do not certify arbitrary mounts; non-ASCII Windows/macOS semantics remain `unknown` where exact filesystem tables are unavailable.

## Install and run

Download a binary and SHA-256 checksums from [Releases](https://github.com/rad1092/treeport/releases), or build with Go 1.25+:

```sh
go install github.com/rad1092/treeport/cmd/treeport@v0.1.1
treeport version
treeport scan --profile windows --root 'C:\export\release' ./dist
treeport zip --profile windows --root 'C:\export\release' --json release.zip
```

All flags precede the input path. `--root` means the future destination root, not the input directory. It must be an absolute drive or UNC path for `windows`; it need not exist on the machine running Treeport.

A manifest can represent a tree the current filesystem cannot store:

```sh
printf '%s\n' '{"path":"CAFÉ/a?b.txt"}' '{"path":"cafe\u0301/a*b.txt"}' |
  treeport manifest --profile export-fold --json -
```

This exits `1` and reports both the parent alias and the full-path collision. Use `--max-component` and `--max-path` to match an export pipeline's explicit budgets; profile defaults and units are [documented](docs/profiles.md).

| Exit | Meaning |
| --- | --- |
| `0` | Known compatible with the selected model |
| `1` | At least one definite incompatibility |
| `2` | Invalid arguments, unreadable/malformed input, limit exceeded, cancellation, or runtime error |
| `3` | Uncertain semantics or symlink behavior; no definite incompatibility found |

A nonzero result should stop a strict CI gate. Inspect `complete` as well: diagnostic truncation is explicitly marked and never becomes a success. [CI and pre-commit examples](docs/integrations.md) retain JSON reports for diagnosis.

## Embed in Go

```go
report, err := treeport.Check(ctx, []treeport.Entry{
    {Path: "café/one.txt", Kind: "file"},
    {Path: "cafe\u0301/two.txt", Kind: "file"},
}, treeport.Options{Profile: "export-fold"})
```

`Check` does not mutate the input slice or touch the filesystem. See the [runnable example](examples/embed/main.go) and [input adapters](docs/contract.md). A context cancels cooperative work; resource budgets bound entries, indexed parents, raw path bytes, and diagnostics.

## Why a tree check?

Normalization differences have caused [rclone filter mismatches](https://github.com/rclone/rclone/issues/7757) and [backend path failures](https://github.com/rclone/rclone/issues/8042). Syncthing documents [case conflicts that stop synchronization](https://docs.syncthing.net/users/syncing.html#case-sensitivity-in-file-names). These are evidence of repeated cross-system filename problems, not endorsements of Treeport.

Existing tools are useful: pre-commit already checks case conflicts and illegal Windows names; CrossRename supports recursive dry-runs and length handling; Pathologize provides path sanitization. Treeport adds a read-only tree contract around combined transformations, destination root budgets, explained groups, and manifest/archive inputs. The [independent corpus and pinned executions](docs/comparison.md) show concrete differences and give existing tools credit for what they already catch.

## Scope and development

Treeport examines names and tree structure. It does not validate contents, destination permissions, free space, cloud-provider rules, existing destination files, link targets, or races with a changing source. No universal portability or archive-security guarantee is made. Read [limitations](docs/limitations.md) before using a report as a release gate.

```sh
go test ./...
go test -race ./...
go vet ./...
go run ./examples/embed
```

The maintained corpus covers NFC/NFD, Korean, emoji UTF-16 budgets, fullwidth distinctions, superscript reserved names, invalid UTF-8, implicit parents, duplicates, and traversal. See [verification](docs/verification.md) for measured environments, CI scope, and reproducible stress runs. [MIT license](LICENSE).
