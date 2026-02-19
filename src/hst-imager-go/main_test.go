package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootCommandContainsExpectedCommands(t *testing.T) {
	root := BuildRootCommand()
	expected := []string{"list", "info", "read", "write", "compare", "transfer", "mbr", "gpt", "rdb", "fs", "adf", "settings"}
	for _, name := range expected {
		if root.Find(name) == nil {
			t.Fatalf("expected command %q to exist", name)
		}
	}
}

func TestNestedRdbPartCommand(t *testing.T) {
	root := BuildRootCommand()
	rdb := root.Find("rdb")
	if rdb == nil {
		t.Fatal("rdb command missing")
	}
	part := rdb.Find("part")
	if part == nil {
		t.Fatal("rdb part command missing")
	}
	if part.Find("format") == nil {
		t.Fatal("rdb part format command missing")
	}
}

func TestRunAcceptsGlobalOptionsWithValues(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"--log-file", "hst.log", "--format", "json", "list"}, &out)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(out.String(), "\"entries\"") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestRunErrorsOnMissingGlobalOptionValue(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"--log-file"}, &out)
	if err == nil {
		t.Fatal("expected error for missing --log-file value")
	}
	if !strings.Contains(err.Error(), "missing value for global option") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunErrorsOnUnsupportedFormat(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"--format", "yaml", "list"}, &out)
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPrintsGlobalHelp(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"--help"}, &out)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(out.String(), "Global options:") {
		t.Fatalf("expected global help output, got %q", out.String())
	}
}

func TestBlankInfoTransferCompareFlow(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.img")
	dst := filepath.Join(tmp, "dst.img")

	var out bytes.Buffer
	if err := run([]string{"blank", src, "1KB"}, &out); err != nil {
		t.Fatalf("blank failed: %v", err)
	}

	info, err := os.Stat(src)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Size() != 1024 {
		t.Fatalf("expected 1024 bytes, got %d", info.Size())
	}

	out.Reset()
	if err := run([]string{"transfer", src, dst}, &out); err != nil {
		t.Fatalf("transfer failed: %v", err)
	}
	if !strings.Contains(out.String(), "Transferred") {
		t.Fatalf("unexpected transfer output: %q", out.String())
	}

	out.Reset()
	if err := run([]string{"compare", src, dst}, &out); err != nil {
		t.Fatalf("compare failed: %v", err)
	}
	if !strings.Contains(out.String(), "Compare successful") {
		t.Fatalf("unexpected compare output: %q", out.String())
	}

	out.Reset()
	if err := run([]string{"info", dst}, &out); err != nil {
		t.Fatalf("info failed: %v", err)
	}
	if !strings.Contains(out.String(), "Size: 1024") {
		t.Fatalf("unexpected info output: %q", out.String())
	}
}

func TestJSONFormatForInfo(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "x.bin")
	if err := os.WriteFile(p, []byte{1, 2, 3, 4}, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"--format", "json", "info", p}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\"size\": 4") {
		t.Fatalf("unexpected json info output: %q", out.String())
	}
}

func TestCompareDetectsDifference(t *testing.T) {
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.bin")
	b := filepath.Join(tmp, "b.bin")

	if err := os.WriteFile(a, []byte{1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte{1, 9, 3}, 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := run([]string{"compare", a, b}, &out)
	if err == nil {
		t.Fatal("expected compare error for differing files")
	}
	if !strings.Contains(err.Error(), "compare failed") {
		t.Fatalf("unexpected compare error: %v", err)
	}
}
