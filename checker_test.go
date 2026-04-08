package main

import "testing"

func TestScanDiffPatternsReportsHighPrecisionSignals(t *testing.T) {
	rawDiff := "" +
		"diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -10,2 +10,5 @@ func run() {\n" +
		" contextLine()\n" +
		"+// TODO: remove this workaround\n" +
		"+//nolint:errcheck\n" +
		"+_ = err\n"

	findings := ScanDiffPatterns(rawDiff)
	if len(findings) != 3 {
		t.Fatalf("ScanDiffPatterns() returned %d findings, want 3: %#v", len(findings), findings)
	}

	want := []string{
		"TODO introduced (main.go:+11)",
		"lint/type suppression introduced (main.go:+12)",
		"error swallowed with `_ = err` (main.go:+13)",
	}
	for i, message := range want {
		if findings[i].Message != message {
			t.Fatalf("finding[%d] = %q, want %q", i, findings[i].Message, message)
		}
	}
}

func TestScanDiffPatternsIgnoresRemovedLines(t *testing.T) {
	rawDiff := "" +
		"diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -20,2 +20,2 @@ func run() {\n" +
		"-// TODO: old debt\n" +
		"+return nil\n"

	findings := ScanDiffPatterns(rawDiff)
	if len(findings) != 0 {
		t.Fatalf("ScanDiffPatterns() returned unexpected findings: %#v", findings)
	}
}

func TestScanDiffPatternsDeduplicatesSameSignalPerLine(t *testing.T) {
	rawDiff := "" +
		"diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,1 +1,2 @@\n" +
		"+// TODO FIXME both words on one line\n"

	findings := ScanDiffPatterns(rawDiff)
	if len(findings) != 1 {
		t.Fatalf("ScanDiffPatterns() returned %d findings, want 1: %#v", len(findings), findings)
	}
	if findings[0].Message != "TODO introduced (main.go:+1)" {
		t.Fatalf("unexpected finding: %#v", findings[0])
	}
}

func TestScanDiffPatternsIgnoresCommentAndStringFalsePositives(t *testing.T) {
	rawDiff := "" +
		"diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,1 +1,4 @@\n" +
		"+fmt.Println(\"nolint and _ = err in docs\")\n" +
		"+// example string: _ = err\n" +
		"+const note = \"type: ignore\"\n" +
		"+return err\n"

	findings := ScanDiffPatterns(rawDiff)
	if len(findings) != 0 {
		t.Fatalf("expected no findings for comment/string false positives, got %#v", findings)
	}
}

func TestScanDiffPatternsSkipsDocumentationFiles(t *testing.T) {
	rawDiff := "" +
		"diff --git a/README.md b/README.md\n" +
		"--- a/README.md\n" +
		"+++ b/README.md\n" +
		"@@ -1,1 +1,2 @@\n" +
		"+Use `//nolint` in this example only.\n"

	findings := ScanDiffPatterns(rawDiff)
	if len(findings) != 0 {
		t.Fatalf("expected documentation diff to be ignored, got %#v", findings)
	}
}
