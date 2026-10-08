# Security

Treeport 0.1.x receives security fixes on the default branch. Please include the tool version, profile/options, OS, and a minimal manifest or synthetic ZIP reproducer when reporting a problem. Do not attach private filenames or archive contents without removing sensitive information.

Report a potential vulnerability privately through [GitHub's security advisory form](https://github.com/rad1092/treeport/security/advisories/new) when that channel is available. If private reporting is unavailable, open a minimal issue requesting a private contact without publishing exploit details or sensitive data. Non-security correctness bugs can use ordinary issues.

The CLI performs no network requests and never renames, copies, or extracts the source. Input adapters bound entry counts, names, metadata, and index work; the checker supports cancellation. Those properties do not make hostile archives safe to extract or concurrently changing filesystem trees safe to trust. Treeport does not verify payloads or defend a later writer against traversal, symlink, permission, or path races.

Keep nonzero exits and `complete:false` visible in integrations. Interpret exact original names from base64, and escape displayed paths in any web UI or log sink that embeds a report. For precise scope, see [limitations](docs/limitations.md).
