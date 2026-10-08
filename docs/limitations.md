# Limits and guarantees

Treeport is a filename preflight, not a copy engine, sanitizer, extractor, malware scanner, or universal portability certificate. A `known-compatible` report means that a complete input satisfied the selected model and budgets. It does not establish that a later write will succeed.

The destination root is caller-supplied context for path length. Treeport does not inspect that mount, existing destination contents, ACLs, free space, filesystem flags, quotas, reserved metadata names beyond the documented rule set, cloud-provider encodings, 8.3 aliases, locale-specific comparison, or hardlink identity. Windows extended/device namespaces are unsupported. The macOS and Windows profiles conservatively leave non-ASCII filesystem semantics unknown.

Scanning reads names and metadata. It does not follow ordinary symlink entries, read file contents, or evaluate symlink targets. An enumeration failure fails the scan; it does not yield a misleading success for an incomplete subtree. The path can change between metadata checks and directory reads: Treeport does not promise race-free traversal or containment against a concurrent hostile writer. Use a trusted, quiescent source or a snapshot when that matters.

ZIP mode reads archive metadata and entry names, never decompresses file payloads. Traversal, absolute names, duplicate entries, and file/directory obstructions are filename diagnostics. Encrypted payloads, compression ratios, payload size, CRC correctness, exploitability, and extraction software behavior are outside scope. Entry-count and metadata budgets constrain the work Treeport performs; they do not make an archive safe to extract.

The input and index are in memory. Entry count, indexed parent count, individual path bytes, aggregate path bytes, and output diagnostics have explicit limits. They bound input dimensions, not an exact resident-memory ceiling. Deep paths may hit the parent-node limit before the entry limit. Cancellation is cooperative; OS filesystem reads are not guaranteed to be immediately interruptible. For a hard CPU/RSS limit, run the CLI within the limits of your CI/container/job system.

Conflict IDs and sorted output are deterministic for the same multiset of entries, options, and versions. IDs are diagnostic labels, not cryptographic identities or an authorization mechanism. A report can be truncated when diagnostic limits are reached; check `complete` before relying on it. Never ignore a non-nil library error and interpret the accompanying report as complete.

Original path bytes are authoritative in `base64`; display strings replace invalid UTF-8 and can look identical for distinct byte sequences. Unicode spelling is not normalized in those original fields. Never reconstruct a source path from the transformed target or display field.
