# Changelog

## 0.1.1 — 2026-10-08

- Reject Windows console aliases CONIN$/CONOUT$ and space-padded device basenames before extensions.
- Advance windows/export-fold profile revisions to 2; retain profile 1 identity for posix/macos.
- Add positive and negative regression cases for these documented reserved names.

## 0.1.0 — 2026-10-08

Initial release of the read-only Go library and CLI.

- Whole-tree and implicit-parent collision groups with stable IDs and explanations.
- Version 1 `posix`, `windows`, `macos`, and explicit `export-fold` destination models.
- Destination-root-inclusive path budgets and UTF-16-aware length handling.
- Directory, strict JSONL, and metadata-only ZIP input; original byte preservation.
- Compatible/incompatible/unknown results, JSON/human reports, exit codes, cancellation, and resource limits.
- Independent regression corpus, pinned baseline comparison, embedding example, pre-commit/CI integration, and cross-platform verification workflow.

Unknown filesystem semantics and path races remain explicit limitations. See [verification evidence](docs/verification.md) for tested environments and [profiles](docs/profiles.md) for the exact model contract.
