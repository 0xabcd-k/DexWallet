#!/usr/bin/env python3
"""Behavior checks for the real generator, without MySQL or application services."""
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

import generate


class CodegenTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="dexwallet-codegen-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        for directory in ("backend", "scripts", "frontend/packages/api-contract"):
            shutil.copytree(generate.ROOT / directory, self.root / directory,
                            ignore=shutil.ignore_patterns("node_modules", "__pycache__"))
        (self.root / ".tools").symlink_to(generate.ROOT / ".tools", target_is_directory=True)

    def test_repeat_generation_is_identical_and_preserves_handwritten_code(self):
        first, scaffold = generate.render(self.root)
        second, second_scaffold = generate.render(self.root)
        self.assertEqual(first, second)
        self.assertEqual(scaffold, second_scaffold)
        logic = self.root / generate.API_OUT / "internal/logic/example/echo_logic.go"
        model = self.root / generate.MODEL_OUT / "contract_sample_model.go"
        handler = self.root / generate.PLATFORM_OUT / "internal/handler/health/live_handler.go"
        handler.write_bytes(handler.read_bytes() + b"\n// handwritten extension\n")
        before = [logic.read_bytes(), model.read_bytes(), handler.read_bytes()]
        generate.synchronize(self.root, first, scaffold)
        self.assertEqual(before, [logic.read_bytes(), model.read_bytes(), handler.read_bytes()])
        self.assertFalse((self.root / generate.PLATFORM_OUT / "internal/config/config.go").exists())
        generate.synchronize(self.root, first, scaffold, check=True)

    def test_new_handler_uses_project_errors_without_manual_changes(self):
        source = self.root / "backend/manifest/api/platform/platform.api"
        source.write_text(source.read_text() + '''
type TemplateRequest {
    Value string `json:"value,options=ready"`
}
@server(group: template)
service platform {
    @handler check
    post /template/check (TemplateRequest) returns (HealthResponse)
}
''')
        # Recreate startup and context as well: generated scaffolds must compile
        # together and use the shared container, including its error handling.
        (self.root / generate.PLATFORM_OUT / "platform.go").unlink()
        (self.root / generate.PLATFORM_OUT / "internal/svc/service_context.go").unlink()
        output, scaffold = generate.render(self.root)
        generate.synchronize(self.root, output, scaffold)
        test = self.root / generate.PLATFORM_OUT / "internal/server/generated_handler_test.go"
        test.write_text('''package server

import (
    "net/http/httptest"
    "testing"
)

func TestGeneratedRequestHandler(t *testing.T) {
    s, _ := setup(t)
    ts := httptest.NewServer(s.HTTP.Handler)
    defer ts.Close()
    for _, body := range []string{"{", "{}", `{"value":"invalid"}`} {
        status, value, _ := request(t, ts.Client(), "POST", ts.URL+"/template/check", body)
        if status != 400 || value["code"] != "INVALID_ARGUMENT" {
            t.Fatalf("generated handler: status=%d body=%v", status, value)
        }
        if _, ok := value["data"]; ok {
            t.Fatal("parse failure must not include success data")
        }
    }
}
''')
        result = subprocess.run(
            ["go", "test", "./api", "./api/internal/server", "-run", "^TestGeneratedRequestHandler$", "-count=1"],
            cwd=self.root / "backend", capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_platform_contract_generates_codes_and_preserves_entrypoint(self):
        source = self.root / "backend/manifest/api/platform/platform.api"
        source.write_text(source.read_text().replace("|PAYLOAD_TOO_LARGE", "|PAYLOAD_TOO_LARGE|EXAMPLE_FAILURE"))
        entry = self.root / "backend/api/platform.go"
        before = entry.read_bytes()
        output, scaffold = generate.render(self.root)
        self.assertIn(b'ExampleFailure', output["backend/apperror/codes_gen.go"])
        self.assertIn(b'EXAMPLE_FAILURE', output["frontend/packages/api-contract/src/platform.ts"])
        generate.synchronize(self.root, output, scaffold)
        self.assertEqual(before, entry.read_bytes())

    def test_source_addition_and_removal_are_visible(self):
        source = self.root / "backend/manifest/api/examples/contract.api"
        original = source.read_text()
        source.write_text(original.replace('Message string', 'Extra string `json:"extra"`\n    Message string'))
        output, scaffold = generate.render(self.root)
        self.assertIn(b"extra: string", output[str(generate.TS_OUT)])
        with self.assertRaisesRegex(RuntimeError, "differs"):
            generate.synchronize(self.root, output, scaffold, check=True)
        generate.synchronize(self.root, output, scaffold)
        source.write_text(original)
        output, scaffold = generate.render(self.root)
        self.assertNotIn(b"extra:", output[str(generate.TS_OUT)])
        generate.synchronize(self.root, output, scaffold)

    def test_config_missing_or_incorrect_cannot_fall_back_to_float(self):
        config = self.root / "backend/goctl.yaml"
        original = config.read_text()
        config.unlink()
        with self.assertRaisesRegex(RuntimeError, "goctl.yaml is required"):
            generate.render(self.root)
        config.write_text(original.replace("decimal.Decimal", "float64").replace("decimal.NullDecimal", "sql.NullFloat64"))
        with self.assertRaisesRegex(RuntimeError, "DECIMAL mapping failed"):
            generate.render(self.root)

    def test_nullable_api_requires_an_explicit_supported_contract(self):
        source = self.root / "backend/manifest/api/examples/contract.api"
        source.write_text(source.read_text().replace("Note string", "Note *string"))
        with self.assertRaisesRegex(RuntimeError, "Unsupported nullable"):
            generate.render(self.root)

    def test_local_generated_edits_are_not_overwritten(self):
        output, scaffold = generate.render(self.root)
        path = self.root / generate.TS_OUT
        path.write_text(path.read_text() + "// local work\n")
        before = path.read_bytes()
        with self.assertRaisesRegex(RuntimeError, "Locally modified"):
            generate.synchronize(self.root, output, scaffold)
        self.assertEqual(before, path.read_bytes())

    def test_untracked_generated_file_is_detected(self):
        output, scaffold = generate.render(self.root)
        path = self.root / generate.MODEL_OUT / "stale_model_gen.go"
        path.write_text("// Code generated by goctl. DO NOT EDIT.\n")
        with self.assertRaisesRegex(RuntimeError, "Unregistered generated file"):
            generate.synchronize(self.root, output, scaffold, check=True)

    def test_retired_generated_files_removed_but_handwritten_files_require_review(self):
        output, scaffold = generate.render(self.root)
        added = dict(output)
        name = str(generate.TS_OUT.parent / "removed.ts")
        added[name] = b"// generated obsolete type\n"
        generate.synchronize(self.root, added, scaffold)
        with self.assertRaisesRegex(RuntimeError, "differs"):
            generate.synchronize(self.root, output, scaffold, check=True)
        generate.synchronize(self.root, output, scaffold)
        self.assertFalse((self.root / name).exists())
        remaining = dict(scaffold)
        retired = next(iter(remaining))
        del remaining[retired]
        with self.assertRaisesRegex(RuntimeError, "Retired handwritten"):
            generate.synchronize(self.root, output, remaining)
        self.assertTrue((self.root / retired).exists())


if __name__ == "__main__":
    unittest.main(verbosity=2)
