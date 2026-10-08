#!/usr/bin/env python3
"""Run small pinned comparisons. No upstream source changes or source-tree writes.

Usage: python3 docs/compare_baselines.py PRECOMMIT_DIR CROSSRENAME_DIR PATHOLOGIZE_DIR GO
Clone the exact revisions documented in comparison.md first. Uses temporary Git
indexes (so NFC/NFD names work even on a normalizing host) and temporary empty files.
The output records behavior; it is not a universal competitor ranking.
"""
import contextlib
import importlib.util
import io
import json
import logging
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True

PINS = ["ccfd9947817b7c328abdbe1b9867adbae8fc6843", "7d8e0568bddd05dd0db8ac23920a25a6d53e358c", "1a30a09a9ec903c0d4152af1dfbbd2f964ccebc8"]
pc, cr, pp = [Path(x).resolve() for x in sys.argv[1:4]]
go = str(Path(sys.argv[4]).resolve())
for root, pin in zip([pc, cr, pp], PINS):
    actual = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()
    if actual != pin:
        raise SystemExit(f"revision mismatch for {root}: {actual} != {pin}")
sys.path.insert(0, str(pc))
from pre_commit_hooks.check_case_conflict import find_conflicting_filenames
spec = importlib.util.spec_from_file_location("crossrename_baseline", cr / "src/crossrename/rename.py")
rename = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rename)
config = (pc / ".pre-commit-hooks.yaml").read_text()
block = config.split("-   id: check-illegal-windows-names\n", 1)[1].split("\n-   id:", 1)[0]
pattern = next(line.split("files: '", 1)[1][:-1] for line in block.splitlines() if "files: '" in line)
illegal = re.compile(pattern)
corpus = json.loads((Path(__file__).resolve().parents[1] / "testdata/corpus.json").read_text())
selected = ["ascii-parent-alias", "normalization-parent-alias", "korean-canonical-equivalence", "composed-transformations", "replacement-collision", "truncation-collision", "windows-root-budget"]
records = []
for f in corpus:
    if f["name"] not in selected:
        continue
    paths = [e["path"] for e in f["entries"]]
    with tempfile.TemporaryDirectory(prefix="treeport-baseline-") as tmp:
        def git(*args, **kwargs):
            return subprocess.check_output(["git", "-C", tmp, *args], **kwargs)
        git("init", "-q")
        git("config", "core.quotePath", "false")
        oid = git("hash-object", "-w", "--stdin", input=b"").decode().strip()
        git("update-index", "--index-info", input="".join(f"100644 {oid}\t{p}\n" for p in paths).encode())
        previous = os.getcwd()
        try:
            os.chdir(tmp)
            capture = io.StringIO()
            with contextlib.redirect_stdout(capture):
                exit_code = find_conflicting_filenames(paths)
        finally:
            os.chdir(previous)
    records.append({"fixture":f["name"], "precommit_case_exit":exit_code,
        "precommit_case_output":capture.getvalue().splitlines(),
        "precommit_illegal_paths":[p for p in paths if illegal.search(p)],
        "crossrename_component_outputs":["/".join(rename.sanitize_filename(c) for c in p.split("/")) for p in paths]})

# Actual CrossRename dry-run functions, with two pending destinations that do not
# exist yet. Neither mutates the files or calls the network/version-check code.
with tempfile.TemporaryDirectory(prefix="treeport-crossrename-") as tmp:
    output = io.StringIO()
    handler = logging.StreamHandler(output)
    rename.logger.addHandler(handler)
    rename.logger.setLevel(logging.INFO)
    for name in ["report?.txt", "report*.txt"]:
        Path(tmp, name).touch()
    for name in ["report?.txt", "report*.txt"]:
        rename.rename_file(Path(tmp, name), dry_run=True)
    dry_run = output.getvalue().splitlines()
    unchanged = sorted(p.name for p in Path(tmp).iterdir()) == ["report*.txt", "report?.txt"]
    rename.logger.removeHandler(handler)

# Execute Pathologize's public CleanPath API at the pinned local checkout.
with tempfile.TemporaryDirectory(prefix="treeport-pathologize-") as tmp:
    Path(tmp, "go.mod").write_text("module baseline\n\ngo 1.25.0\n\nrequire github.com/spf13/pathologize v0.0.0\nreplace github.com/spf13/pathologize => " + str(pp) + "\n")
    Path(tmp, "main.go").write_text('''package main
import ("encoding/json"; "os"; "github.com/spf13/pathologize")
func main(){ var paths []string; if err:=json.NewDecoder(os.Stdin).Decode(&paths);err!=nil{panic(err)}; out:=make([]string,len(paths));for i,p:=range paths{out[i]=pathologize.CleanPath(p)};if err:=json.NewEncoder(os.Stdout).Encode(out);err!=nil{panic(err)} }
''')
    for row in records:
        f = next(f for f in corpus if f["name"] == row["fixture"])
        raw = subprocess.check_output([go, "run", "."], cwd=tmp, input=json.dumps([e["path"] for e in f["entries"]]).encode(), env={**os.environ, "GOTOOLCHAIN":"local", "GOWORK":"off"})
        row["pathologize_cleanpath_outputs"] = json.loads(raw)
go_version = subprocess.check_output([go, "version"], text=True).strip()
print(json.dumps({"pins":dict(zip(["precommit", "crossrename", "pathologize"],PINS)), "python":sys.version.split()[0], "go":go_version, "records":records, "crossrename_actual_dry_run":dry_run, "dry_run_source_unchanged":unchanged},ensure_ascii=False,indent=2))
