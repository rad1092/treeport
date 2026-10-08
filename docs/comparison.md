# Independent corpus and measured comparison

Treeport's initial 20-case [regression corpus](../testdata/corpus.json) and [black-box test](../corpus_test.go) were written before its checker implementation. The expected outcomes come from the documented models, not from recording the implementation's output. Every fixture is also checked in reverse enumeration order, and caller entries must remain unchanged.

On 2026-10-08, `TestIndependentRegressionCorpus` passed all 20 cases locally. The cases include NFC/NFD parents, mixed normalization/case/replacement, UTF-16 emoji truncation, Windows root budgets, Korean equivalence, fullwidth-versus-ASCII distinctions, superscript device names, invalid bytes, duplicate entries, file/directory obstructions, and unsafe archive paths.

## Pinned baselines

The comparison executes upstream code at these revisions. No upstream source is modified:

| Project | Revision | Executed surface |
| --- | --- | --- |
| [pre-commit-hooks](https://github.com/pre-commit/pre-commit-hooks/tree/ccfd9947817b7c328abdbe1b9867adbae8fc6843) | `ccfd9947817b7c328abdbe1b9867adbae8fc6843` | Real `find_conflicting_filenames`; actual illegal-Windows-name hook regex |
| [CrossRename](https://github.com/Jemeni11/CrossRename/tree/7d8e0568bddd05dd0db8ac23920a25a6d53e358c) | `7d8e0568bddd05dd0db8ac23920a25a6d53e358c` | Real `sanitize_filename` and `rename_file(dry_run=True)` |
| [Pathologize](https://github.com/spf13/pathologize/tree/1a30a09a9ec903c0d4152af1dfbbd2f964ccebc8) | `1a30a09a9ec903c0d4152af1dfbbd2f964ccebc8` | Public Go `CleanPath` API |

[Machine-readable results](comparison-results.json) were produced by [compare_baselines.py](compare_baselines.py). The script verifies revision IDs; uses real temporary Git indexes so host normalization does not erase fixture names; calls the unmodified hook function; uses the exact `language: fail` regex from upstream YAML; and executes the sanitizer APIs. It does not pretend the regex is a separate Python command or run a substitute implementation of upstream rules. Python 3.14.6 and Go 1.25 were used on macOS. The wrapper's CLI/package-install behavior is outside this comparison.

| Case | Existing behavior observed | Treeport behavior |
| --- | --- | --- |
| `Docs/one.txt`, `docs/two.txt` | pre-commit **already detects** parent case conflict | Same core issue, grouped with explicit parent cause |
| `café/one.txt`, `cafe\u0301/two.txt` | Both hooks pass; both sanitizer APIs leave spelling unchanged | `export-fold` finds a normalization parent alias |
| `한글.txt`, decomposed Hangul spelling | Both hooks pass; both sanitizer APIs leave spelling unchanged | `export-fold` finds canonical-equivalence collision |
| `CAFÉ/a?b.txt`, `cafe\u0301/a*b.txt` | Illegal-name hook **already rejects** names; sanitizers remove `?`/`*` but retain distinct parent spellings | Reports the composed normalization/case/replacement collision and parent group |
| `report?.txt`, `report*.txt` | Illegal-name hook **already rejects** names; sanitizer APIs produce the same `report.txt` | Reports the many-to-one group under the declared `_` replacement pipeline |
| `abcdefgh-one`, `abcdefgh-two`, custom component budget 8 | Hooks pass; sanitizers with their default budgets leave names unchanged | Explicit budget 8 produces a truncation conflict; this is a configurable-policy comparison, not a claim that CrossRename lacks truncation |
| `한국어/😀.txt`, root `C:\export\release`, total budget 25 | Hooks pass; sanitizer APIs leave names unchanged | Reports destination-root-inclusive path-budget failure |

The first two normalization rows are concrete additional detections over the chosen baseline surfaces. They prove useful added coverage for the explicit transformation contract, not that generic Unicode folding exactly predicts APFS or NTFS. Windows/macOS candidate comparisons remain unknown.

In a separate actual dry-run on two empty temporary source files, CrossRename logged both `report?.txt → report.txt` and `report*.txt → report.txt` without warning that the two pending destinations coincide. Neither destination existed yet. The source files remained unchanged. This is a future-destination planning case; CrossRename does have logic for a target that already exists, recursive traversal, byte-length truncation, and directory renaming. We do not claim it lacks those features.

Pathologize's contract is individual path cleaning. A caller can compare its outputs and build a tree check around it. The measured same-output cases are a reason to retain whole-tree context around sanitizers, not an assertion that an individual-path API should independently know its callers' other paths.

## Reproduce

Clone the three repositories into temporary directories and check out the exact revisions above, then run:

```sh
python3 docs/compare_baselines.py /tmp/pre-commit-hooks /tmp/CrossRename /tmp/pathologize "$(command -v go)"
go test -run TestIndependentRegressionCorpus -v .
```

The comparison creates and removes its own temporary indexes and empty source files. It does not download dependencies or modify the checked-out baseline sources. Large benchmark inputs are separate from this small maintained corpus.

## Evidence of the problem

Rclone users reported [normalization-sensitive filtering](https://github.com/rclone/rclone/issues/7757) and a [macOS/backend path mismatch](https://github.com/rclone/rclone/issues/8042). Rclone documents [restricted filename encodings](https://rclone.org/overview/#restricted-filenames), so its transformation policy must not be assumed to equal Treeport's example export policy. Syncthing describes [case conflicts requiring a consistent chosen name](https://docs.syncthing.net/users/syncing.html#case-sensitivity-in-file-names). These references establish recurring boundary problems; they are not claims that Treeport integrates with, replaces, or is endorsed by those projects.
