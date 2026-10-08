# Versioned destination profiles

A profile is a reproducible model selected by the caller. Treeport does not discover the destination filesystem. Each JSON report includes `profile`, `profile_version`, and `unicode_version`; Unicode normalization and folding reuse `golang.org/x/text` rather than a custom Unicode table. Version 0.1.2 pins x/text v0.28.0, using Unicode 15.0.0 and reporting the normalization table version at runtime.

| Profile | Comparison / transformation | Component default | Full path default |
| --- | --- | --- | --- |
| `posix` | Exact raw bytes, case sensitive | 255 bytes | 4095 bytes |
| `windows` | Case-insensitive Win32-oriented model; trims terminal ASCII dot/space for alias detection | 255 UTF-16 code units | 259 UTF-16 code units |
| `macos` | NFC + Unicode case-fold candidate for a case-insensitive destination | 255 UTF-8 bytes | 1023 UTF-8 bytes |
| `export-fold` | Explicit pipeline below, independent of a real filesystem | 255 UTF-16 code units | 259 UTF-16 code units |

The full path budget includes the supplied destination root, one separator, and the relative path. `windows` and `macos` measure original components: case-fold expansion is a comparison operation and does not imply a longer physical filename. `posix` measures raw bytes; `export-fold` measures the actual transformed path. A trailing separator on the root is counted once. `posix` and `macos` recognize only `/` as that separator: a literal trailing backslash remains part of the root name and consumes its full byte budget. `windows` uses its validated backslash root syntax; `export-fold` accepts either slash or backslash separators for its synthetic destination-root accounting. This destination-root syntax is separate from the stricter slash-delimited input-entry grammar. Budgets exclude a terminating NUL. `MaxComponent`/`--max-component` and `MaxPath`/`--max-path` override the profile defaults; `0` means the default. They do not auto-detect long-path support. Windows requires a fully qualified drive or UNC root and validates its component budgets; device and extended namespaces are unsupported. Other profiles allow an empty root to measure only the relative path.

## Explicit export pipeline

For each component, `export-fold` performs these steps in order:

1. NFC normalization.
2. Unicode default case folding with x/text.
3. Replace Windows forbidden characters and ASCII control characters with `_`.
4. Remove trailing ASCII spaces and dots.
5. Truncate to the selected UTF-16 component budget without splitting a Unicode scalar.

Treeport compares the resulting full paths **and every parent prefix**. It does not write transformed names. Reserved Windows device names, empty/dot components, and trailing dot/space reintroduced by truncation are incompatibilities; the pipeline is not a universally safe sanitizer. Truncation is intentionally a final stage, not a fixpoint sanitization routine.

NFC is not NFKC: fullwidth `Ａ` and ASCII `A` remain distinct after folding, while precomposed and decomposed Hangul converge. An emoji outside the BMP uses two UTF-16 units. The pipeline is useful only if it matches the export contract being checked; rclone and other tools may use different transformations.

## Known and unknown semantics

`windows` is a conservative policy based on Microsoft's documented shell filename conventions, not a syscall emulator. An actual GitHub Windows runner allowed `COM¹` through Go `os.OpenFile`; the profile intentionally rejects that documented reserved spelling. API-specific acceptance does not establish portability to other Windows tools.

`windows` diagnoses forbidden characters, trailing ASCII dot/space, and reserved devices including `COM¹`–`COM³` and `LPT¹`–`LPT³`, with extensions. Its ASCII case aliases are definite within the selected case-insensitive model. Non-ASCII comparison uses Unicode folding only to find candidates, returns `unknown`, and is not a claim about NTFS `$UpCase`, per-directory case flags, Win32 namespaces, network shares, or a particular Windows API. See Microsoft's [filename conventions](https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file) and [path-length documentation](https://learn.microsoft.com/en-us/windows/win32/fileio/maximum-file-path-limitation).

`macos` models ASCII case-insensitive names and treats non-ASCII comparison as unknown. NFC/folding candidates do not implement APFS/HFS+ tables. Apple documents distinct case modes and versioned normalization behavior in its [archived APFS guide](https://developer.apple.com/library/archive/documentation/FileManagement/Conceptual/APFS_Guide/FAQ/FAQ.html). A local filesystem probe verifies only the current test mount; it does not upgrade this profile into an exact APFS implementation.

`posix` is a byte-sensitive lexical model with declared budgets, not a description of every Unix mount. Backslashes are rejected as unsafe manifest syntax even though some POSIX filesystems permit them as filename bytes. All profiles reject absolute paths, drive prefixes, NUL, empty components, and `.`/`..` segments. Explicit directory records may end with one `/`.

Invalid UTF-8 is retained as raw bytes for `posix`. Unicode profiles report `invalid_utf8` as unknown, while still detecting exact-byte duplicate entries as definite conflicts and checking valid parent prefixes before the invalid component. Symlinks are indexed by name but their target behavior is unknown. These uncertainties cannot be cleared by selecting a larger length budget.

For normalization terminology see [Unicode Standard Annex #15](https://unicode.org/reports/tr15/). Profile behavior changes require a new profile version; schema changes are tracked separately.

## Profile revisions

In Treeport 0.1.1, `posix` and `macos` remained at model version 1. Treeport 0.1.1 uses model version 2 for `windows` and `export-fold`: it also rejects `CONIN$`, `CONOUT$`, and device basenames padded with ASCII spaces before an extension (for example `CON .txt`). The console aliases are documented in [CreateFile's console rules](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilea#consoles). This is a conservative policy across Windows tools; newer individual APIs may accept particular extension variants. Version 0.1.0/model 1 did not diagnose those spellings. Group IDs include the selected model revision.

Treeport 0.1.2 advances `posix` and `macos` to model version 2, preserving literal backslashes in destination-root length accounting. `windows` and `export-fold` remain at model version 2. Earlier releases incorrectly trimmed trailing backslashes from POSIX/macOS roots and could undercount their full-path budgets.
