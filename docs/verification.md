# Verification scope

The maintained regression corpus and tests run on the release commit. Check the [CI workflow](https://github.com/rad1092/treeport/actions/workflows/ci.yml) for the exact SHA and job results; a badge or cross-compilation alone is not evidence that a binary ran on a target OS.

The independent 20-case corpus was run successfully on macOS arm64 on 2026-10-08. It also reverses fixture enumeration and compares serialized reports. The [pinned baseline comparison](comparison.md) was executed on the same date; its compact output is committed. The local native filesystem probe observed `case_sensitive=false` and `normalization_sensitive=false`. This verifies that observed volume behavior, not a case-sensitive APFS volume or all macOS filesystems.

Repository checks include Go tests, race detection, vet, fuzz seeds, CLI installed-binary smoke, read-only input behavior, strict manifest errors, ZIP name checks, resource budgets, and filesystem probes where the host permits them. A native filesystem test must record its observed case mode instead of assuming that all macOS or Windows volumes have one mode. Tests that require permissions or a particular filesystem capability may skip with a reason; such a skip does not verify that capability.

See the workflow and release evidence for the final native Linux, Windows, and macOS results. Each environment exercises the model and its source reader. This does not certify every filesystem on that OS, Windows per-directory case sensitivity, both APFS variants, network filesystems, or host-specific Unicode tables. Unknown semantics in the product remain unknown after CI passes.

Million-entry synthetic runs must generate input in temporary storage and retain only a compact summary of entry count, budgets, duration, peak memory if measured, versions, and result. The generated input and stress dumps are not release assets. Do not infer an exact RSS limit from a raw-byte budget: index and runtime overhead also consume memory.

Before release:

```sh
go test ./...
go test -race ./...
go vet ./...
go run ./examples/embed
```

The release builder and checksums support reproducible inspection of packaged binary targets. Download the archive appropriate to the actual host, verify `SHA256SUMS`, run `treeport version`, and run one compatible and one incompatible manifest against that installed binary. Record the commit that produced the artifact alongside CI evidence.
