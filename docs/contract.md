# Library and JSON contract

The module is `github.com/rad1092/treeport`. The initial public API and report schema are versioned separately from destination profiles. Import the root package for comparison, and `/input` for read-only input adapters.

```go
report, err := treeport.Check(ctx, entries, treeport.Options{
    Profile:         "windows",
    DestinationRoot: `C:\exports\release`,
    Limits:          treeport.DefaultLimits(),
})
```

`ctx` must be non-nil. `Entry` has `Path string` and `Kind string`: `file`, `directory`, or `symlink`; an empty kind defaults to `file` in `Check`. A Go string may contain arbitrary bytes. Paths are relative and slash-delimited on every host OS. One final slash is accepted for an explicit directory. The checker synthesizes missing parent directories; callers do not need to list them.

Options contain `Profile`, `DestinationRoot`, `MaxComponent`, `MaxPath`, and `Limits`. Profile/budget semantics are in [profiles.md](profiles.md). `Check` neither mutates entries nor reads the filesystem. Shared callers may run independent checks concurrently. Treat the report as unusable when `err != nil`; input, configuration, node/input-budget, and cancellation errors do not constitute a completed scan.

The input package exposes:

```go
input.Tree(ctx, sourceDirectory, limits)       // ([]treeport.Entry, error)
input.Manifest(ctx, reader, limits)           // ([]treeport.Entry, error)
input.ZIP(ctx, archiveFilename, limits)        // ([]treeport.Entry, error)
```

Adapters return no partial result on error. Tree enumeration includes descendants and excludes its root. Symlinks are entries; a symlink source root is rejected. Special files are rejected. No Git ignore rules are applied, so scan a staged export directory or provide an explicit manifest when you need a selected set of names. The destination root is unrelated to the tree source root.

## JSONL input

Each line contains exactly one object with exactly one of `path` or `path_base64`, and an optional `kind` (default `file`). Empty lines, duplicate or unknown fields, non-string values, malformed UTF-8/JSON, unpaired Unicode surrogate escapes, and trailing JSON are rejected. A syntactically valid but unsafe path is accepted into the name checker and diagnosed there.

```jsonl
{"path":"docs/안내.txt","kind":"file"}
{"path":"assets/","kind":"directory"}
{"path_base64":"/2E=","kind":"file"}
```

The last line represents bytes `ff 61`, not the string `/2E=`. Base64 is canonical padded RFC 4648 standard encoding. Use this field for POSIX filename bytes that are not UTF-8. Do not pass both path fields. Manifest filenames and stdin use the same parser.

ZIP parsing uses stored entry-name bytes without extraction. Name decoding from legacy archive encodings is outside the contract: a non-UTF-8 name remains raw bytes, not a guessed CP437 or local code-page string. Symlink metadata produces a `symlink` entry; targets are not read. Directory names and file/directory duplicate structures remain observable.

## Resource limits

Zero-valued library limits use defaults; negative limits fail. CLI limits must be positive. Defaults are 1,000,000 entries, 2,000,000 indexed nodes including implicit parents, 4096 raw bytes per path, 128 MiB aggregate raw path bytes, 512 MiB of index accounting, and 10,000 issues and 10,000 conflict groups. `MaxIndexBytes` / `--max-index-bytes` accounts for 128 bytes plus original and transformed prefix lengths per indexed node; this is a deterministic work-budget estimate, not a hard RSS limit. See `treeport scan --help` for every limit flag. Encoded manifest and ZIP metadata overhead are bounded separately by the input adapter.

Input/index budget failures return an error. A diagnostic-list budget truncates the report, sets `complete:false`, and appends a limitation. It never changes an incompatible/unknown result to compatible. Memory use is proportional to bounded paths and index nodes, not constant space. OS reads and arbitrary blocked readers may outlive a cooperative context deadline; close caller-owned pipes/readers when canceling. The CLI has `--timeout` (default `30s`) and handles interrupt cancellation.

## Report schema 1

Successful CLI analysis with `--json` writes one object to stdout:

| Field | Contract |
| --- | --- |
| `schema_version` | String `"1"` |
| `tool_version` | Treeport release version |
| `profile`, `profile_version`, `unicode_version` | Selected model and comparison-table identity |
| `destination_root` | Caller-supplied root used for budget accounting |
| `component_budget`, `path_budget`, `length_unit` | Effective budgets after overrides, and `bytes` or `utf16` |
| `status` | `known-compatible`, `incompatible`, or `unknown` |
| `complete` | Whether diagnostics were retained completely |
| `entry_count`, `node_count` | Input records and indexed explicit/implicit nodes |
| `issues` | Individual path diagnostics |
| `conflicts` | Groups mapping onto one target path/prefix |
| `limitations` | Explicit bounds on interpretation |

A definite incompatibility takes precedence over uncertainty in the overall status. `complete:true` says the bounded input was processed without report truncation; it does not mean filesystem semantics are known. Conversely, an unknown result can be complete.

Each issue has `code`, `status`, `path`, and `detail`. Codes include `reserved_name`, `forbidden_character`, `trailing_dot_space`, `component_budget`, `path_budget`, `invalid_utf8`, `unsafe_path`, `unknown_semantics`, `empty_component`, and `symlink`. Human-readable detail may evolve; branch on codes and status.

Each conflict has `id`, `target`, `status`, `causes`, and `members`. Causes are additive: `case_fold`, `unicode_normalization`, `character_replacement`, `trailing_dot_space`, `truncation`, `parent_alias`, `duplicate_entry`, `file_directory`. They record transformations participating in the group and can include transformations inherited from parents. They are not a minimal formal proof that every transformation was individually necessary.

A member contains `path`, `kind`, `implicit`, `count`, and `uncertain`. `implicit:true` denotes a synthesized parent with no explicit record; its count is `0`. Explicit duplicates are aggregated and preserve their multiplicity in `count`. `uncertain:true` means that member depends on candidate Unicode filesystem semantics. An `incompatible` group contains a definite colliding pair; it may also contain additional uncertain candidates, which retain their member flag. A path/kind pair represented as both `a` and `a/` for directories may use a stable representative spelling; it denotes the same directory and is reported as duplicate input.

Original paths use `{ "display": "…", "base64": "…" }`. **Decode `base64` for exact bytes.** `display` preserves valid Unicode spelling but replaces invalid UTF-8 for readable JSON. The transformed `target` is a display/comparison artifact, not a suggested rename or recoverable source path. `treeport.OriginalPath` provides the same byte-preserving serialization for embedding clients.

Issues, groups, and members have stable order for the same multiset of records. Group IDs remain stable under enumeration reversal; callers should scope stored IDs to tool/profile versions and not treat IDs as permanent global identifiers. Paths, kinds, and duplicate multiplicity contribute to group identity.

CLI errors with `--json` have a smaller envelope: `schema_version`, `tool_version`, `status:"error"`, and `error`. They exit `2` and are not analysis reports. Human output quotes path strings so control characters cannot become terminal instructions. All stdout write errors also fail the command.
