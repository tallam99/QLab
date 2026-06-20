#!/usr/bin/env python3
"""Tests for check-yaak-secrets.py.

Run: python3 scripts/test_check_yaak_secrets.py   (or: python3 -m unittest -v)
Stdlib only — no third-party deps.
"""

import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

# The script's filename has hyphens, so load it by path rather than import.
_DIR = Path(__file__).resolve().parent
_SPEC = importlib.util.spec_from_file_location("check_yaak_secrets", _DIR / "check-yaak-secrets.py")
chk = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(chk)

# A real-looking JWT (header.payload.signature) for "literal secret" cases.
FAKE_JWT = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"


def _workspace(*, auth_value="", extra_headers=None):
    """Build a minimal Yaak workspace dict with one env and one request."""
    return {
        "yaakSchema": 5,
        "resources": {
            "environments": [
                {
                    "model": "environment",
                    "name": "production",
                    "variables": [
                        {"enabled": True, "name": "base_url", "value": "https://example.com"},
                        {"enabled": True, "name": "auth_token", "value": auth_value},
                    ],
                }
            ],
            "httpRequests": [
                {
                    "model": "http_request",
                    "name": "healthz",
                    "method": "GET",
                    "url": "${[ base_url ]}/healthz",
                    "headers": extra_headers or [],
                }
            ],
        },
    }


def _check_dict(doc) -> list[str]:
    """Write doc to a temp .yaak.json and run check_file on it."""
    with tempfile.NamedTemporaryFile("w", suffix=".yaak.json", delete=False) as f:
        json.dump(doc, f)
        path = Path(f.name)
    try:
        return chk.check_file(path)
    finally:
        path.unlink()


class IsSecretName(unittest.TestCase):
    def test_names(self):
        cases = {
            "auth_token": True,
            "Authorization": True,
            "api_key": True,
            "client_secret": True,
            "PASSWORD": True,
            "base_url": False,
            "confirm": False,
            "": False,
        }
        for name, want in cases.items():
            with self.subTest(name=name):
                self.assertEqual(chk.is_secret_name(name), want)


class LooksLikeSecret(unittest.TestCase):
    def test_literals_are_flagged(self):
        for value in [FAKE_JWT, "Bearer abcdef1234567890", "AIza" + "x" * 35, "sk-" + "a" * 20]:
            with self.subTest(value=value[:12]):
                self.assertIsNotNone(chk.looks_like_secret(value))

    def test_safe_values_pass(self):
        for value in ["", "Bearer ${[ auth_token ]}", "${[ auth_token ]}", "${[ base_url ]}/healthz", "GET"]:
            with self.subTest(value=value):
                self.assertIsNone(chk.looks_like_secret(value))


class CheckFile(unittest.TestCase):
    def test_clean_passes(self):
        self.assertEqual(_check_dict(_workspace()), [])

    def test_template_header_passes(self):
        headers = [{"enabled": True, "name": "Authorization", "value": "Bearer ${[ auth_token ]}"}]
        self.assertEqual(_check_dict(_workspace(extra_headers=headers)), [])

    def test_literal_token_in_secret_field_fails(self):
        problems = _check_dict(_workspace(auth_value=FAKE_JWT))
        self.assertTrue(problems)
        joined = "\n".join(problems)
        self.assertIn("auth_token", joined)  # Rule 1: secret-named field non-empty
        self.assertIn("JWT", joined)         # Rule 2: value looks like a JWT

    def test_literal_bearer_in_header_fails(self):
        headers = [{"enabled": True, "name": "Authorization", "value": "Bearer abcdef1234567890"}]
        self.assertTrue(_check_dict(_workspace(extra_headers=headers)))

    def test_missing_file(self):
        self.assertTrue(chk.check_file(Path("/nonexistent/nope.yaak.json")))


class CommittedWorkspaceIsClean(unittest.TestCase):
    """Regression guard: the actual committed workspace must always pass."""

    def test_repo_workspace(self):
        workspace = _DIR.parent / "yaak" / "qlab.yaak.json"
        if not workspace.exists():
            self.skipTest("committed workspace not found")
        self.assertEqual(chk.check_file(workspace), [])


if __name__ == "__main__":
    unittest.main()
