# CI and pre-commit

Whole-tree checks need the complete export tree or manifest. A changed-file-only invocation can miss a collision with an unchanged sibling or parent. Treeport's hook always scans its configured tree and does not accept the pre-commit changed-filename list.

## pre-commit

```yaml
repos:
  - repo: https://github.com/rad1092/treeport
    rev: v0.1.3
    hooks:
      - id: treeport
        args: [--profile, windows, --root, 'C:\checkout', .]
```

This scans working-tree names, including untracked and ignored files. Build/export into a dedicated folder and change the last argument when that is your release boundary. Git's index can represent names the current working tree cannot; use a generated JSONL manifest when the index or an archive is your authoritative input.

For an already installed binary, a local hook can use:

```yaml
repos:
  - repo: local
    hooks:
      - id: treeport-export
        name: Check export destination tree
        entry: treeport scan --profile windows --root 'C:\release' ./dist
        language: system
        pass_filenames: false
        always_run: true
```

## GitHub Actions

```yaml
name: Destination filenames
on: [push, pull_request]
permissions:
  contents: read
jobs:
  preflight:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25.x'
      - run: go install github.com/rad1092/treeport/cmd/treeport@v0.1.3
      # Create dist here, then check its destination namespace.
      - name: Check export
        run: treeport scan --profile windows --root 'C:\release' --json ./dist > treeport-report.json
      - uses: actions/upload-artifact@v4
        if: always()
        with:
          name: treeport-report
          path: treeport-report.json
```

The command fails the job on incompatible (`1`), error (`2`), or unknown (`3`). Keep that distinction in a wrapper if your policy routes unknown results to review. Do not turn all nonzero exits into success. Pin third-party actions and Treeport to your organization's approved immutable revisions for controlled builds.

Scan the ZIP you will ship with `treeport zip … release.zip`, or stream your exporter manifest to `treeport manifest … -`. A source tree can be valid while the exporter changes names; select `export-fold` only when its [exact pipeline](profiles.md) matches your exporter.

Treeport needs no credentials, network access, destination mount, or write permission to source content at runtime. Installing a binary or resolving Go modules does require the usual build/download access.
