"""CI boundary regressions: prose, consumers, schemas, renames and main."""
from pathlib import Path
import runpy
import unittest

class ScopeTests(unittest.TestCase):
    def test_scope_boundaries(self):
        classify = runpy.run_path(str(Path(__file__).with_name("ci-scope.py")))["classify"]
        cases = [
            (["docs/agent-ops/git-workflow.md"], True, False, False),
            (["README.md", "AGENTS.md"], True, False, False),
            (["docs/.vitepress/pages/index.md"], True, True, True),
            (["docs/.vitepress/theme/index.ts"], True, True, True),
            (["src/ui/renderer.ts"], True, True, True),
            (["styles.css"], True, True, True),
            (["tests/gallery/main.ts"], True, True, True),
            (["pnpm-lock.yaml"], True, True, True),
            ([".github/workflows/ci.yml"], True, True, True),
            (["openapi/source.json"], True, True, True),
            (["docs/api/openapi.yaml"], True, True, True),
            (["migrations/010.sql"], True, True, True),
            (["internal/http/handler.go"], True, True, True),
            (["internal/http/handler_test.go"], True, True, False),
            (["Dockerfile", "docs/release.md"], True, True, True),
            (["go.mod", "go.sum"], True, True, True),
            (["unknown-path"], True, True, True),
            ([], True, True, True),
            (["README.md"], False, True, True),
            # A runtime -> docs rename includes both paths, even if the old file is gone.
            (["src/retired.ts", "docs/retired.md"], True, True, True),
            (["docs/removed.md"], True, False, False),
            (["docs/file with spaces.md"], True, False, False),
        ]
        for paths, is_pr, full, docker in cases:
            with self.subTest(paths=paths, is_pr=is_pr):
                actual = classify(paths, is_pr)
                self.assertEqual(actual["full"], full)
                if "docker" in actual:
                    self.assertEqual(actual["docker"], docker)

if __name__ == "__main__":
    unittest.main()
