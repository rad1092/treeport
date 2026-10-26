# Treeport

[한국어](README.ko.md) · [Profiles](docs/profiles.md) · [JSON/API contract](docs/contract.md) · [Measured comparison](docs/comparison.md)

**Find the file names that will break on another operating system before you ship a ZIP, export a folder, or someone clones your repository on Windows.** Treeport reads a Git index, a folder, a ZIP or a JSONL list, and reports names that collide or are invalid on the destination you name: `README.md` next to `Readme.md`, `aux.c`, a trailing dot, a path over 260 characters, or a Korean name written in decomposed Unicode on a Mac. It never renames, copies, or extracts files.

```text
CAFÉ/a?b.txt
café/a*b.txt
       ↓ export-fold: NFC → case fold → replace → trim → truncate
café/a_b.txt     collision: normalization + case + replacement
```

The two parents also converge, even when their children have different names. Reports include those implicit parent conflicts, duplicate entries, file/directory obstructions, and exact original bytes.

Choose a destination **model** explicitly: `posix`, `windows`, `macos`, or the precisely specified `export-fold` transformation pipeline. Results are `known-compatible`, `incompatible`, or `unknown`. Where exact Windows or macOS tables are unavailable for non-ASCII names, the result is `unknown` with a hint about what the destination most likely does.

## Install and run

Download a binary and SHA-256 checksums from [Releases](https://github.com/rad1092/treeport/releases), or build with Go 1.25+:

```sh
go install github.com/rad1092/treeport/cmd/treeport@v0.2.0
treeport version
treeport git --profile windows --root 'C:\src\project'      # this repository, as a Windows clone
treeport scan --profile windows --root 'C:\export\release' ./dist
treeport zip --profile windows --root 'C:\export\release' --json release.zip
```

`treeport git` reads `git ls-files`, so it checks the names a clone creates, not whatever happens to be in your working tree. It is also available as a [pre-commit hook and a GitHub Action](docs/integrations.md):

```yaml
# .pre-commit-config.yaml
repos:
  - repo: https://github.com/rad1092/treeport
    rev: v0.2.0
    hooks:
      - id: treeport

# .github/workflows/names.yml, after actions/checkout
      - uses: rad1092/treeport@v0.2.0
        with:
          profile: windows
          root: 'C:\src\project'
```

All flags precede the input path. `--root` means the future destination root, not the input directory. It must be an absolute drive or UNC path for `windows`; it need not exist on the machine running Treeport.

A manifest can represent a tree the current filesystem cannot store:

```sh
printf '%s\n' '{"path":"CAFÉ/a?b.txt"}' '{"path":"cafe\u0301/a*b.txt"}' |
  treeport manifest --profile export-fold --json -
```

This exits `1` and reports both the parent alias and the full-path collision. Use `--max-component` and `--max-path` to match an export pipeline's explicit budgets; profile defaults and units are [documented](docs/profiles.md).

For a known-good first check, replace the manifest with
`{"path":"reports/week-42.csv"}` and use the same command: expect exit `0` and
`status: "known-compatible"`. [A complete first-run example](docs/first-run.md)
covers passing and failing reports, Windows paths, and common installation errors.
The public Go API is already indexed on
[pkg.go.dev](https://pkg.go.dev/github.com/rad1092/treeport).

| Exit | Meaning |
| --- | --- |
| `0` | Known compatible with the selected model, or unknown but very likely compatible |
| `1` | At least one definite incompatibility, or unknown names that very likely collide |
| `2` | Invalid arguments, unreadable/malformed input, limit exceeded, cancellation, or runtime error |
| `3` | Unknown and Treeport cannot tell which way (symlinks, invalid UTF-8, truncation) |

`--strict` turns every unknown result into exit `3`, ignoring the likely verdict. The JSON `status` stays `unknown` either way and `likely` says which way the hints point. Inspect `complete` as well: diagnostic truncation is explicitly marked and never becomes a success. [CI and pre-commit examples](docs/integrations.md) retain JSON reports for diagnosis.

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

[Third-party licenses and pinned provenance](third_party/README.md) accompany source and binary distributions.
