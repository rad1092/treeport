# Third-party distribution materials

Treeport's own source is distributed under the [MIT license](../LICENSE). The
third-party texts in this directory accompany the source and every official
binary archive starting with v0.1.3. Earlier releases receive a separate disclosure
supplement; their original binaries and tags are unchanged.

The [manifest](manifest.json) records the source URL and SHA-256 of each copied
text. Upstream LICENSE and PATENTS files are copied byte for byte. Additional Go
source attribution excerpts retain their original comment text. These excerpts
are distribution materials prepared by Treeport, not upstream files called NOTICE.
The pinned x/text and Go roots contain LICENSE and PATENTS; neither has a root
NOTICE file.

## Pinned components

| Component | Revision | Distribution scope |
| --- | --- | --- |
| `golang.org/x/text` | v0.28.0, commit `425d715b4a85c7698cedf621412bb53794cbda53` | Direct module dependency; Unicode folding and normalization |
| Go | go1.27.1, commit `862c888e612ac346c7c4d99c9392bdfd265f33b0` | Runtime and standard-library code used in the official binaries |

The x/text module checksum is
`h1:rhazDwis8INMIwQ4tpjLDzUhx6RlXqZNPEM0huQojng=` (see `go.sum`). Its LICENSE and
PATENTS are retained under `golang.org-x-text/`. Go's LICENSE and PATENTS are
retained separately under `go/` even where the bytes are identical.

Official archives use Go 1.27.1 with `CGO_ENABLED=0` and the six combinations of
`darwin`, `linux`, `windows` with `amd64`, `arm64`. The Go compiler/toolchain itself
is not included in the archives. The disclosure inventory covers the runtime,
standard library, dependency, and derived-data materials described here; it is
not an inventory of every package shipped inside the Go compiler distribution.

## Go source attributions

A `go list -deps -json ./cmd/treeport` audit with the six target environments and
`CGO_ENABLED=0` examined 126 packages and 1,091 distinct selected source, assembly,
and header files. This is a conservative set of compiler inputs, not a claim
that every function survives linker elimination. The same disclosure set is
included in each target archive.

`go/attributions/` preserves unchanged source excerpts for Lucent/Vita Nuova's
Inferno memmove, Sun fdlibm (including both SunPro and SunSoft notice variants),
Stephen Moshier's Cephes, the public-domain Rijndael reference, and the original
SLEEF provenance. It also retains short upstream attributions for SHA-512 arm64,
Keccak, TCMalloc, Windows CLDR zones, pdqsort, and Hacker's Delight. Those latter
excerpts are source provenance; no separate license is inferred from an
algorithm citation. The source fragment URLs and byte hashes are in the manifest;
[the source audit](https://github.com/rad1092/treeport/blob/v0.1.3/docs/license-audit.json)
records the selection method and full-source hashes.

No `src/vendor` or `src/cmd/vendor` package was selected. The selected
`crypto/internal/boring` files use the disabled `notboring.go` implementation;
BoringSSL objects and cgo sources are not included in these release builds.

## Unicode data

The selected x/text source files `cases/tables15.0.0.go` and
`unicode/norm/tables15.0.0.go` identify Unicode 15.0.0; its language tables identify
CLDR 32. The generators use Unicode UCD and CLDR data. Go 1.27.1's standard-library
`unicode/tables.go` identifies Unicode 17.0.0. The inventory therefore also retains
these unchanged Unicode data license texts:

- `unicode/cldr32-unicode-license.txt`: CLDR release-32's complete Unicode license
  (1991–2017), including its original UTF-8 BOM.
- `unicode/unicode-1991-2022-icu72-section.txt`: the complete opening Unicode
  license section (lines 1–47) from ICU release-72-1's LICENSE, preserving the
  historical 1991–2022 notice from the Unicode 15 era. This is an excerpt of the
  Unicode license, not an assertion that ICU code ships in Treeport.
- `unicode/unicode-current-license.txt`: Unicode License v3 (1991–2026), retrieved
  from Unicode's license endpoint on 2026-10-08. This endpoint is unversioned;
  the manifest pins the exact retrieved bytes. Historical texts remain alongside
  it rather than being replaced by the current copyright year.

The source URLs and fragment ranges are in the manifest. No Unicode data tables
were changed by this packaging patch.

## Packaging checks

The release builder validates the manifest hashes and inventory, resolved module
versions, absence of module replacements, and the exact Go version. Before
archiving each executable, it also inspects its embedded build information for
the expected dependency, commit, target, and CGO setting. Every archive includes
this directory, and `build.json` records the disclosure-manifest digest. Tests
check both ZIP and tar.gz payloads as well as missing or modified disclosures.

Build from a clean checkout with the pinned Go toolchain, including VCS metadata
for the release utility itself:

```sh
go run -buildvcs=true ./scripts/release -out dist
```

When updating a dependency, Go version, build flags, or release targets, review
its actual upstream texts and selected source files, update this inventory and
its hashes, and rebuild the archives. A local `go install` can use a different
toolchain or build settings and is outside the pinned official-build inventory.
