# CI and pre-commit

Whole-tree checks need the complete export tree or manifest. A changed-file-only invocation can miss a collision with an unchanged sibling or parent. Treeport's hook always scans its configured tree and does not accept the pre-commit changed-filename list.

## pre-commit

```yaml
repos:
  - repo: https://github.com/rad1092/treeport
    rev: v0.2.0
    hooks:
      - id: treeport
        # The default is a Windows clone at C:\checkout. Any profile works:
        args: [--profile, windows, --root, 'C:\src\project']
```

The hook runs `treeport git`, which checks every name in the Git index: the names a clone creates, including files staged in this commit, without untracked or ignored files. It ignores the pre-commit changed-file list on purpose, because a new name can collide with an unchanged one.

To check a build folder with an installed binary instead:

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
name: File names
on: [push, pull_request]
permissions:
  contents: read
jobs:
  names:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: rad1092/treeport@v0.2.0
        with:
          profile: windows
          root: 'C:\src\project'
```

The action builds Treeport from the tagged source, runs `treeport git` on the checkout, writes the result to the job summary, and exposes `status`, `likely` and `report` (the JSON path) as outputs. Set `command: scan` and `input: dist` to check a build folder, or `command: zip` and `input: release.zip`. `strict: true` fails on every unknown result.

Without the action:

```yaml
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25.x'
      - run: go install github.com/rad1092/treeport/cmd/treeport@v0.2.0
      - name: Check export
        run: treeport scan --profile windows --root 'C:\release' --json ./dist > treeport-report.json
```

The command fails the job on incompatible (`1`), error (`2`), or an unknown result Treeport cannot read either way (`3`). Unknown results that very likely pass exit `0`; add `--strict` if your policy routes every unknown result to review. Do not turn all nonzero exits into success. Pin third-party actions and Treeport to your organization's approved immutable revisions for controlled builds.

Scan the ZIP you will ship with `treeport zip … release.zip`, or stream your exporter manifest to `treeport manifest … -`. A source tree can be valid while the exporter changes names; select `export-fold` only when its [exact pipeline](profiles.md) matches your exporter.

Treeport needs no credentials, network access, destination mount, or write permission to source content at runtime. Installing a binary or resolving Go modules does require the usual build/download access.
