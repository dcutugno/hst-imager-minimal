package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootCommandContainsExpectedCommands(t *testing.T) {
	root := BuildRootCommand()
	expected := []string{"list", "info", "read", "write", "compare", "transfer", "mbr", "gpt", "rdb", "fs", "adf", "settings", "archive", "script"}
	for _, name := range expected {
		if root.Find(name) == nil {
			t.Fatalf("expected command %q to exist", name)
		}
	}
}

func TestRunListJsonOutput(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"--format", "json", "list"}, &out)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(out.String(), "\"drives\"") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestSettingsUpdateAndList(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var out bytes.Buffer
	if err := run([]string{"settings", "update", "foo", "bar"}, &out); err != nil {
		t.Fatalf("settings update failed: %v", err)
	}
	out.Reset()
	if err := run([]string{"settings", "list"}, &out); err != nil {
		t.Fatalf("settings list failed: %v", err)
	}
	if !strings.Contains(out.String(), "foo=bar") {
		t.Fatalf("unexpected settings output: %q", out.String())
	}
}

func TestFsDirAndArchiveListAndScript(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"fs", "dir", tmp}, &out); err != nil {
		t.Fatalf("fs dir failed: %v", err)
	}
	if !strings.Contains(out.String(), "a.txt") {
		t.Fatalf("unexpected fs dir output: %q", out.String())
	}

	zipPath := filepath.Join(tmp, "test.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	w, err := zw.Create("inside.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("data"))
	_ = zw.Close()
	_ = zf.Close()

	out.Reset()
	if err := run([]string{"archive", "list", zipPath}, &out); err != nil {
		t.Fatalf("archive list failed: %v", err)
	}
	if !strings.Contains(out.String(), "inside.txt") {
		t.Fatalf("unexpected archive list output: %q", out.String())
	}

	scriptPath := filepath.Join(tmp, "run.txt")
	script := "blank " + filepath.Join(tmp, "s.img") + " 1KB\ninfo " + filepath.Join(tmp, "s.img") + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"script", scriptPath}, &out); err != nil {
		t.Fatalf("script failed: %v", err)
	}
	if !strings.Contains(out.String(), "Created blank image") || !strings.Contains(out.String(), "Path:") {
		t.Fatalf("unexpected script output: %q", out.String())
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
	out.Reset()
	if err := run([]string{"compare", src, dst}, &out); err != nil {
		t.Fatalf("compare failed: %v", err)
	}
	out.Reset()
	if err := run([]string{"info", dst}, &out); err != nil {
		t.Fatalf("info failed: %v", err)
	}
	if !strings.Contains(out.String(), "Size: 1024") {
		t.Fatalf("unexpected info output: %q", out.String())
	}
}
