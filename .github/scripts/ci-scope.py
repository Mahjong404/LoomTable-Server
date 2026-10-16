"""Only known Markdown prose avoids the full CI gate."""
import json
import os
from pathlib import Path
import subprocess


def is_prose(path):
    return path in {"README.md", "AGENTS.md", "CONTRIBUTING.md", "LICENSE"} or (
        path.startswith("docs/")
        and path.endswith(".md")
        and not path.startswith("docs/.vitepress/")
    )


def classify(paths, is_pr):
    full = not is_pr or not paths or any(not is_prose(p) for p in paths)
    docker = not is_pr or not paths or any(
        not is_prose(p) and not p.endswith("_test.go") for p in paths
    )
    return {"full": full, "docker": docker}


def main():
    is_pr = os.environ["GITHUB_EVENT_NAME"] == "pull_request"
    head = os.environ["HEAD_SHA"]
    base = (
        subprocess.check_output(
            ["git", "merge-base", os.environ["BASE_SHA"], head], text=True
        ).strip()
        if is_pr
        else head + "^"
    )
    # Classify both sides of renames; deleted runtime paths must not look like docs.
    changed = subprocess.check_output(
        ["git", "diff", "--no-renames", "--name-only", "-z", base, head]
    )
    paths = [p for p in changed.decode().split("\0") if p]
    subprocess.run(["git", "diff", "--check", base, head], check=True)
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
        for key, value in classify(paths, is_pr).items():
            output.write(f"{key}={str(value).lower()}\n")
    prose = [p for p in paths if p.endswith(".md") and Path(p).is_file()]
    Path(os.environ["RUNNER_TEMP"], "ci-prose.json").write_text(
        json.dumps(prose), encoding="utf-8"
    )


if __name__ == "__main__":
    main()
