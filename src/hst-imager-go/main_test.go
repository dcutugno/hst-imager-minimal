package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ulikunitz/xz"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("HST_IMAGER_LEGACY_MODE", "off")
	os.Exit(m.Run())
}

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
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "operation not permitted") || strings.Contains(lower, "permission denied") || strings.Contains(lower, "exit status") {
			t.Skipf("list command unavailable in this environment: %v", err)
		}
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(out.String(), "\"drives\"") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestRunListJsonOutputShortFormatFlag(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"-f", "json", "list"}, &out)
	if err != nil {
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "operation not permitted") || strings.Contains(lower, "permission denied") || strings.Contains(lower, "exit status") {
			t.Skipf("list command unavailable in this environment: %v", err)
		}
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(out.String(), "\"drives\"") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestSettingsUpdateAndList(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("HOME", configRoot)
	t.Setenv("APPDATA", configRoot)
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

	tarGzPath := filepath.Join(tmp, "test.tar.gz")
	if err := writeTarGzArchive(tarGzPath, map[string]string{
		"nested/hello.txt": "hello",
		"root.txt":         "root",
	}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"archive", "list", tarGzPath}, &out); err != nil {
		t.Fatalf("archive list tar.gz failed: %v", err)
	}
	if !strings.Contains(out.String(), "nested/hello.txt") || !strings.Contains(out.String(), "root.txt") {
		t.Fatalf("unexpected tar.gz archive list output: %q", out.String())
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

func TestFsAndArchiveCommandAliases(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"fs", "d", src}, &out); err != nil {
		t.Fatalf("fs d alias failed: %v", err)
	}
	if !strings.Contains(out.String(), "a.txt") {
		t.Fatalf("unexpected fs d output: %q", out.String())
	}

	dst := filepath.Join(tmp, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"fs", "c", filepath.Join(src, "a.txt"), dst}, &out); err != nil {
		t.Fatalf("fs c alias failed: %v", err)
	}
	copied, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	if err != nil {
		t.Fatalf("expected copied file from fs c alias: %v", err)
	}
	if string(copied) != "aaa" {
		t.Fatalf("unexpected fs c copied content: %q", copied)
	}

	zipPath := filepath.Join(tmp, "sample.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	w, err := zw.Create("inner.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("inner-data")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zf.Close(); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := run([]string{"arc", "l", zipPath}, &out); err != nil {
		t.Fatalf("arc l aliases failed: %v", err)
	}
	if !strings.Contains(out.String(), "inner.txt") {
		t.Fatalf("unexpected arc l output: %q", out.String())
	}

	extractDst := filepath.Join(tmp, "extract")
	out.Reset()
	if err := run([]string{"fs", "x", zipPath, extractDst}, &out); err != nil {
		t.Fatalf("fs x alias failed: %v", err)
	}
	extracted, err := os.ReadFile(filepath.Join(extractDst, "inner.txt"))
	if err != nil {
		t.Fatalf("expected extracted file from fs x alias: %v", err)
	}
	if string(extracted) != "inner-data" {
		t.Fatalf("unexpected fs x extracted content: %q", extracted)
	}
}

func TestCommandNormalizationDoesNotRewriteArguments(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()

	var out bytes.Buffer
	if err := run([]string{"fs", "mkdir", "init"}, &out); err != nil {
		t.Fatalf("fs mkdir init failed: %v", err)
	}
	if err := run([]string{"fs", "mkdir", "del"}, &out); err != nil {
		t.Fatalf("fs mkdir del failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "init")); err != nil {
		t.Fatalf("expected directory init to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "del")); err != nil {
		t.Fatalf("expected directory del to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "initialize")); err == nil {
		t.Fatal("did not expect directory initialize to be created")
	}
	if _, err := os.Stat(filepath.Join(tmp, "delete")); err == nil {
		t.Fatal("did not expect directory delete to be created")
	}
}

func TestFsExtractTarGzNative(t *testing.T) {
	tmp := t.TempDir()
	tarGzPath := filepath.Join(tmp, "sample.tar.gz")
	if err := writeTarGzArchive(tarGzPath, map[string]string{
		"dir/a.txt": "aaa",
		"dir/b.txt": "bbb",
		"c.txt":     "ccc",
	}); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(tmp, "out")
	var out bytes.Buffer
	if err := run([]string{"fs", "extract", tarGzPath, dest, "--recursive"}, &out); err != nil {
		t.Fatalf("fs extract tar.gz failed: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "dir", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "aaa" {
		t.Fatalf("unexpected extracted content: %q", string(b))
	}
}

func TestArchiveListTarXzNative(t *testing.T) {
	tmp := t.TempDir()
	tarXzPath := filepath.Join(tmp, "sample.tar.xz")
	if err := writeTarXzArchive(tarXzPath, map[string]string{
		"dir/a.txt": "aaa",
		"root.txt":  "root",
	}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"archive", "list", tarXzPath}, &out); err != nil {
		t.Fatalf("archive list tar.xz failed: %v", err)
	}
	if !strings.Contains(out.String(), "dir/a.txt") || !strings.Contains(out.String(), "root.txt") {
		t.Fatalf("unexpected tar.xz archive list output: %q", out.String())
	}
}

func TestFsExtractTarXzNative(t *testing.T) {
	tmp := t.TempDir()
	tarXzPath := filepath.Join(tmp, "sample.tar.xz")
	if err := writeTarXzArchive(tarXzPath, map[string]string{
		"dir/a.txt": "aaa",
		"dir/b.txt": "bbb",
	}); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(tmp, "out")
	var out bytes.Buffer
	if err := run([]string{"fs", "extract", tarXzPath + string(os.PathSeparator) + "DIR" + string(os.PathSeparator) + "A.TXT", dest}, &out); err != nil {
		t.Fatalf("fs extract tar.xz failed: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "a.txt"))
	if err != nil {
		t.Fatalf("expected extracted file a.txt: %v", err)
	}
	if string(b) != "aaa" {
		t.Fatalf("unexpected extracted content: %q", string(b))
	}
}

func TestFsExtractArchiveRootWithoutRecursive(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "sample.zip")

	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	w, err := zw.Create("dir/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("aaa")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zf.Close(); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(tmp, "out")
	var out bytes.Buffer
	if err := run([]string{"fs", "extract", zipPath, dest}, &out); err != nil {
		t.Fatalf("fs extract zip root without recursive failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "dir", "a.txt")); err != nil {
		t.Fatalf("expected extracted file dir/a.txt: %v", err)
	}
}

func TestFsExtractArchiveSingleFileWithoutRecursive(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "sample.zip")

	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	w, err := zw.Create("dir/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("aaa")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zf.Close(); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(tmp, "out")
	var out bytes.Buffer
	if err := run([]string{"fs", "extract", zipPath + string(os.PathSeparator) + "Dir" + string(os.PathSeparator) + "A.TXT", dest}, &out); err != nil {
		t.Fatalf("fs extract zip single-file without recursive failed: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "a.txt"))
	if err != nil {
		t.Fatalf("expected extracted file a.txt: %v", err)
	}
	if string(b) != "aaa" {
		t.Fatalf("unexpected extracted content: %q", string(b))
	}
}

func TestArchiveListGzipWorksWithoutBsdtar(t *testing.T) {
	t.Setenv("PATH", "")
	tmp := t.TempDir()
	gzPath := filepath.Join(tmp, "sample.bin.gz")
	payload := []byte("hello gzip")
	if err := writeGzipFileWithName(gzPath, "dir/payload.bin", payload); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"archive", "list", gzPath}, &out); err != nil {
		t.Fatalf("archive list gzip without bsdtar failed: %v", err)
	}
	if !strings.Contains(out.String(), "dir/payload.bin") {
		t.Fatalf("unexpected gzip list output: %q", out.String())
	}
}

func TestFsExtractGzipInnerPathCaseInsensitiveWorksWithoutBsdtar(t *testing.T) {
	t.Setenv("PATH", "")
	tmp := t.TempDir()
	gzPath := filepath.Join(tmp, "sample.bin.gz")
	payload := []byte("hello gzip")
	if err := writeGzipFileWithName(gzPath, "dir/payload.bin", payload); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(tmp, "out")
	source := gzPath + string(os.PathSeparator) + "DIR" + string(os.PathSeparator) + "PAYLOAD.BIN"
	var out bytes.Buffer
	if err := run([]string{"fs", "extract", source, dest}, &out); err != nil {
		t.Fatalf("fs extract gzip without bsdtar failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "payload.bin"))
	if err != nil {
		t.Fatalf("expected extracted file payload.bin: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected extracted content: %q", string(got))
	}
}

func TestArchiveListXzWorksWithoutBsdtar(t *testing.T) {
	t.Setenv("PATH", "")
	xzPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Xz", "test.txt.xz")
	var out bytes.Buffer
	if err := run([]string{"archive", "list", xzPath}, &out); err != nil {
		t.Fatalf("archive list xz without bsdtar failed: %v", err)
	}
	if !strings.Contains(out.String(), "test.txt") {
		t.Fatalf("unexpected xz list output: %q", out.String())
	}
}

func TestFsExtractXzInnerPathCaseInsensitiveWorksWithoutBsdtar(t *testing.T) {
	t.Setenv("PATH", "")
	xzPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Xz", "test.txt.xz")
	plainPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Xz", "test.txt")
	want, err := os.ReadFile(plainPath)
	if err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "out")
	source := xzPath + string(os.PathSeparator) + "TEST.TXT"
	var out bytes.Buffer
	if err := run([]string{"fs", "extract", source, dest}, &out); err != nil {
		t.Fatalf("fs extract xz without bsdtar failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "test.txt"))
	if err != nil {
		t.Fatalf("expected extracted file test.txt: %v", err)
	}
	if !bytes.Equal(normalizeLineEndings(got), normalizeLineEndings(want)) {
		t.Fatal("extracted xz file content mismatch")
	}
}

func TestArchiveListLzwWorksWithoutBsdtar(t *testing.T) {
	t.Setenv("PATH", "")
	lzwPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lzw", "test.txt.Z")
	var out bytes.Buffer
	if err := run([]string{"archive", "list", lzwPath}, &out); err != nil {
		t.Fatalf("archive list lzw without bsdtar failed: %v", err)
	}
	if !strings.Contains(out.String(), "test.txt") {
		t.Fatalf("unexpected lzw list output: %q", out.String())
	}
}

func TestFsExtractLzwInnerPathCaseInsensitiveWorksWithoutBsdtar(t *testing.T) {
	t.Setenv("PATH", "")
	lzwPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lzw", "test.txt.Z")
	plainPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lzw", "test.txt")
	want, err := os.ReadFile(plainPath)
	if err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "out")
	source := lzwPath + string(os.PathSeparator) + "TEST.TXT"
	var out bytes.Buffer
	if err := run([]string{"fs", "extract", source, dest}, &out); err != nil {
		t.Fatalf("fs extract lzw without bsdtar failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "test.txt"))
	if err != nil {
		t.Fatalf("expected extracted file test.txt: %v", err)
	}
	if !bytes.Equal(normalizeLineEndings(got), normalizeLineEndings(want)) {
		t.Fatal("extracted lzw file content mismatch")
	}
}

func TestArchiveListBzip2WorksWithoutBsdtar(t *testing.T) {
	tmp := t.TempDir()
	bz2Path := filepath.Join(tmp, "sample.bin.bz2")
	payload := []byte("hello bzip2")
	if err := writeBzip2FileWithSystemTool(t, bz2Path, payload); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", "")
	var out bytes.Buffer
	if err := run([]string{"archive", "list", bz2Path}, &out); err != nil {
		t.Fatalf("archive list bzip2 without bsdtar failed: %v", err)
	}
	if !strings.Contains(out.String(), "sample.bin") {
		t.Fatalf("unexpected bzip2 list output: %q", out.String())
	}
}

func TestFsExtractBzip2InnerPathCaseInsensitiveWorksWithoutBsdtar(t *testing.T) {
	tmp := t.TempDir()
	bz2Path := filepath.Join(tmp, "sample.bin.bz2")
	payload := []byte("hello bzip2")
	if err := writeBzip2FileWithSystemTool(t, bz2Path, payload); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", "")
	dest := filepath.Join(tmp, "out")
	source := bz2Path + string(os.PathSeparator) + "SAMPLE.BIN"
	var out bytes.Buffer
	if err := run([]string{"fs", "extract", source, dest}, &out); err != nil {
		t.Fatalf("fs extract bzip2 without bsdtar failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "sample.bin"))
	if err != nil {
		t.Fatalf("expected extracted file sample.bin: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected extracted content: %q", string(got))
	}
}

func TestFsExtractLzhInnerPathWorksWithoutBsdtar(t *testing.T) {
	t.Setenv("PATH", "")
	tmp := t.TempDir()
	srcLha := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lha", "amiga.lha")
	data, err := os.ReadFile(srcLha)
	if err != nil {
		t.Fatal(err)
	}
	lzhPath := filepath.Join(tmp, "amiga.lzh")
	if err := os.WriteFile(lzhPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(tmp, "out")
	source := lzhPath + string(os.PathSeparator) + "TEST1" + string(os.PathSeparator) + "TEST2.INFO"
	var out bytes.Buffer
	if err := run([]string{"fs", "extract", source, dest}, &out); err != nil {
		t.Fatalf("fs extract lzh inner path without bsdtar failed: %v", err)
	}
	info, err := os.Stat(filepath.Join(dest, "test2.info"))
	if err != nil {
		t.Fatalf("expected extracted file test2.info: %v", err)
	}
	if info.Size() != 900 {
		t.Fatalf("expected test2.info size 900, got %d", info.Size())
	}
}

func TestArchiveListLhaNativeStored(t *testing.T) {
	lhaPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lha", "dirs-files.lha")
	var out bytes.Buffer
	if err := run([]string{"archive", "list", lhaPath}, &out); err != nil {
		t.Fatalf("archive list lha failed: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "dir1/dir3/") || !strings.Contains(got, "dir1/file1.txt") || !strings.Contains(got, "dir2/") {
		t.Fatalf("unexpected lha list output: %q", got)
	}
}

func TestArchiveListLhaStoredWorksWithoutBsdtar(t *testing.T) {
	t.Setenv("PATH", "")
	lhaPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lha", "dirs-files.lha")
	var out bytes.Buffer
	if err := run([]string{"archive", "list", lhaPath}, &out); err != nil {
		t.Fatalf("archive list lha without bsdtar failed: %v", err)
	}
	if !strings.Contains(out.String(), "dir1/file1.txt") {
		t.Fatalf("unexpected lha list output: %q", out.String())
	}
}

func TestArchiveListLhaNativeCompressed(t *testing.T) {
	lhaPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lha", "amiga.lha")
	var out bytes.Buffer
	if err := run([]string{"archive", "list", lhaPath}, &out); err != nil {
		t.Fatalf("archive list lha compressed failed: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "test1.info") || !strings.Contains(got, "test1/test2.info") {
		t.Fatalf("unexpected lha compressed list output: %q", got)
	}
}

func TestFsExtractLhaStoredNative(t *testing.T) {
	tmp := t.TempDir()
	lhaPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lha", "dirs-files.lha")
	dest := filepath.Join(tmp, "out")
	var out bytes.Buffer
	if err := run([]string{"fs", "extract", lhaPath, dest, "--recursive"}, &out); err != nil {
		t.Fatalf("fs extract lha failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "dir1", "dir3")); err != nil {
		t.Fatalf("expected extracted directory dir1/dir3: %v", err)
	}
	info, err := os.Stat(filepath.Join(dest, "dir1", "file1.txt"))
	if err != nil {
		t.Fatalf("expected extracted file dir1/file1.txt: %v", err)
	}
	if info.Size() != 1 {
		t.Fatalf("expected file1.txt size 1, got %d", info.Size())
	}
	if _, err := os.Stat(filepath.Join(dest, "dir2")); err != nil {
		t.Fatalf("expected extracted directory dir2: %v", err)
	}
}

func TestFsExtractLhaStoredWorksWithoutBsdtar(t *testing.T) {
	t.Setenv("PATH", "")
	tmp := t.TempDir()
	lhaPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lha", "dirs-files.lha")
	dest := filepath.Join(tmp, "out")
	var out bytes.Buffer
	if err := run([]string{"fs", "extract", lhaPath, dest, "--recursive"}, &out); err != nil {
		t.Fatalf("fs extract lha without bsdtar failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "dir1", "file1.txt")); err != nil {
		t.Fatalf("expected extracted file dir1/file1.txt: %v", err)
	}
}

func TestFsExtractLhaCompressedNative(t *testing.T) {
	tmp := t.TempDir()
	lhaPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lha", "amiga.lha")
	dest := filepath.Join(tmp, "out")
	var out bytes.Buffer
	if err := run([]string{"fs", "extract", lhaPath, dest, "--recursive"}, &out); err != nil {
		t.Fatalf("fs extract lha compressed failed: %v", err)
	}
	info, err := os.Stat(filepath.Join(dest, "test1.info"))
	if err != nil {
		t.Fatalf("expected extracted file test1.info: %v", err)
	}
	if info.Size() != 900 {
		t.Fatalf("expected test1.info size 900, got %d", info.Size())
	}
	info, err = os.Stat(filepath.Join(dest, "test1", "test2.info"))
	if err != nil {
		t.Fatalf("expected extracted file test1/test2.info: %v", err)
	}
	if info.Size() != 900 {
		t.Fatalf("expected test1/test2.info size 900, got %d", info.Size())
	}
}

func TestDecodeLhaEntryPayloadStoredMethod(t *testing.T) {
	payload := []byte("abc")
	decoded, err := decodeLhaEntryPayload("-lh0-", payload, uint32(len(payload)))
	if err != nil {
		t.Fatalf("decode stored payload failed: %v", err)
	}
	if !bytes.Equal(decoded, payload) {
		t.Fatalf("unexpected decoded payload: %q", decoded)
	}
}

func TestDecodeLhaEntryPayloadCompressedFixture(t *testing.T) {
	lhaPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lha", "amiga.lha")
	data, err := os.ReadFile(lhaPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := parseLhaEntries(data)
	if err != nil {
		t.Fatalf("parseLhaEntries failed: %v", err)
	}
	var compressed *lhaEntry
	for i := range entries {
		if strings.EqualFold(entries[i].Name, "test1/test2.info") {
			compressed = &entries[i]
			break
		}
	}
	if compressed == nil {
		t.Fatalf("expected compressed entry test1/test2.info in parsed entries, got: %+v", entries)
	}
	payload, err := lhaEntryPayload(data, *compressed)
	if err != nil {
		t.Fatalf("lhaEntryPayload failed: %v", err)
	}
	decoded, err := decodeLhaEntryPayload(compressed.Method, payload, compressed.OriginalSize)
	if err != nil {
		t.Fatalf("decodeLhaEntryPayload compressed entry failed: %v", err)
	}
	if len(decoded) != int(compressed.OriginalSize) {
		t.Fatalf("expected decoded size %d, got %d", compressed.OriginalSize, len(decoded))
	}
}

func TestDecodeLhaPayloadJeromSupportsKnownMethodMappings(t *testing.T) {
	cases := []string{"-lh1-", "-lh2-", "-lh3-", "-lh4-", "-lh5-", "-lh6-", "-lh7-", "-lzs-", "-lz5-", "-pm0-", "-pm2-"}
	for _, method := range cases {
		if _, ok := lhaJeromMethodNumber(method); !ok {
			t.Fatalf("expected method mapping for %s", method)
		}
	}
	if _, ok := lhaJeromMethodNumber("-unknown-"); ok {
		t.Fatal("did not expect mapping for unknown method")
	}
}

func TestDecodeLhaPayloadJeromFixtureLh5(t *testing.T) {
	lhaPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lha", "amiga.lha")
	data, err := os.ReadFile(lhaPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := parseLhaEntries(data)
	if err != nil {
		t.Fatalf("parseLhaEntries failed: %v", err)
	}
	var compressed *lhaEntry
	for i := range entries {
		if strings.EqualFold(entries[i].Method, "-lh5-") && strings.EqualFold(entries[i].Name, "test1/test2.info") {
			compressed = &entries[i]
			break
		}
	}
	if compressed == nil {
		t.Fatal("expected -lh5- fixture entry")
	}
	payload, err := lhaEntryPayload(data, *compressed)
	if err != nil {
		t.Fatalf("lhaEntryPayload failed: %v", err)
	}
	decoded, err := decodeLhaPayloadJerom(compressed.Method, payload, int(compressed.OriginalSize))
	if err != nil {
		t.Fatalf("decodeLhaPayloadJerom failed: %v", err)
	}
	if len(decoded) != int(compressed.OriginalSize) {
		t.Fatalf("expected decoded size %d, got %d", compressed.OriginalSize, len(decoded))
	}
}

func TestExtractLhaStoredOnlySupportsCompressedFixture(t *testing.T) {
	tmp := t.TempDir()
	lhaPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lha", "amiga.lha")
	dest := filepath.Join(tmp, "out")
	if err := extractLhaArchiveStoredOnly(lhaPath, "TEST1/TEST2.INFO", dest); err != nil {
		t.Fatalf("extractLhaArchiveStoredOnly compressed fixture failed: %v", err)
	}
	info, err := os.Stat(filepath.Join(dest, "test2.info"))
	if err != nil {
		t.Fatalf("expected extracted file test2.info: %v", err)
	}
	if info.Size() != 900 {
		t.Fatalf("expected test2.info size 900, got %d", info.Size())
	}
}

func TestParseLhaEntriesSupportsLevel1Stored(t *testing.T) {
	tmp := t.TempDir()
	lhaPath := filepath.Join(tmp, "lv1.lha")
	content := []byte("abc")
	if err := writeLhaLevel1StoredFixture(lhaPath, "dir1/file1.txt", content); err != nil {
		t.Fatalf("failed to create level-1 lha fixture: %v", err)
	}
	data, err := os.ReadFile(lhaPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := parseLhaEntries(data)
	if err != nil {
		t.Fatalf("parseLhaEntries level-1 failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one level-1 entry, got %d", len(entries))
	}
	entry := entries[0]
	if !strings.EqualFold(entry.Name, "dir1/file1.txt") {
		t.Fatalf("unexpected level-1 entry name: %q", entry.Name)
	}
	if entry.CompressedSize != uint32(len(content)) || entry.OriginalSize != uint32(len(content)) {
		t.Fatalf("unexpected level-1 sizes: compressed=%d original=%d", entry.CompressedSize, entry.OriginalSize)
	}
}

func TestExtractLhaStoredOnlySupportsLevel1Stored(t *testing.T) {
	tmp := t.TempDir()
	lhaPath := filepath.Join(tmp, "lv1.lha")
	want := []byte("content-level1")
	if err := writeLhaLevel1StoredFixture(lhaPath, "dir1/file1.txt", want); err != nil {
		t.Fatalf("failed to create level-1 lha fixture: %v", err)
	}

	dest := filepath.Join(tmp, "out")
	if err := extractLhaArchiveStoredOnly(lhaPath, "DIR1/FILE1.TXT", dest); err != nil {
		t.Fatalf("extractLhaArchiveStoredOnly level-1 failed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "file1.txt"))
	if err != nil {
		t.Fatalf("expected extracted level-1 file: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected extracted level-1 content: %q", got)
	}
}

func TestParseLhaEntriesSupportsLevel2Stored(t *testing.T) {
	tmp := t.TempDir()
	lhaPath := filepath.Join(tmp, "lv2.lha")
	content := []byte("lv2")
	if err := writeLhaLevel2StoredFixture(lhaPath, "dir2/file2.txt", content); err != nil {
		t.Fatalf("failed to create level-2 lha fixture: %v", err)
	}
	data, err := os.ReadFile(lhaPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := parseLhaEntries(data)
	if err != nil {
		t.Fatalf("parseLhaEntries level-2 failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one level-2 entry, got %d", len(entries))
	}
	entry := entries[0]
	if !strings.EqualFold(entry.Name, "dir2/file2.txt") {
		t.Fatalf("unexpected level-2 entry name: %q", entry.Name)
	}
	if entry.CompressedSize != uint32(len(content)) || entry.OriginalSize != uint32(len(content)) {
		t.Fatalf("unexpected level-2 sizes: compressed=%d original=%d", entry.CompressedSize, entry.OriginalSize)
	}
}

func TestExtractLhaStoredOnlySupportsLevel2Stored(t *testing.T) {
	tmp := t.TempDir()
	lhaPath := filepath.Join(tmp, "lv2.lha")
	want := []byte("content-level2")
	if err := writeLhaLevel2StoredFixture(lhaPath, "dir2/file2.txt", want); err != nil {
		t.Fatalf("failed to create level-2 lha fixture: %v", err)
	}

	dest := filepath.Join(tmp, "out")
	if err := extractLhaArchiveStoredOnly(lhaPath, "DIR2/FILE2.TXT", dest); err != nil {
		t.Fatalf("extractLhaArchiveStoredOnly level-2 failed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "file2.txt"))
	if err != nil {
		t.Fatalf("expected extracted level-2 file: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected extracted level-2 content: %q", got)
	}
}

func TestParseLhaEntriesSupportsLevel3Stored(t *testing.T) {
	tmp := t.TempDir()
	lhaPath := filepath.Join(tmp, "lv3.lha")
	content := []byte("lv3")
	if err := writeLhaLevel3StoredFixture(lhaPath, "dir3/file3.txt", content); err != nil {
		t.Fatalf("failed to create level-3 lha fixture: %v", err)
	}
	data, err := os.ReadFile(lhaPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := parseLhaEntries(data)
	if err != nil {
		t.Fatalf("parseLhaEntries level-3 failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one level-3 entry, got %d", len(entries))
	}
	entry := entries[0]
	if !strings.EqualFold(entry.Name, "dir3/file3.txt") {
		t.Fatalf("unexpected level-3 entry name: %q", entry.Name)
	}
	if entry.CompressedSize != uint32(len(content)) || entry.OriginalSize != uint32(len(content)) {
		t.Fatalf("unexpected level-3 sizes: compressed=%d original=%d", entry.CompressedSize, entry.OriginalSize)
	}
}

func TestExtractLhaStoredOnlySupportsLevel3Stored(t *testing.T) {
	tmp := t.TempDir()
	lhaPath := filepath.Join(tmp, "lv3.lha")
	want := []byte("content-level3")
	if err := writeLhaLevel3StoredFixture(lhaPath, "dir3/file3.txt", want); err != nil {
		t.Fatalf("failed to create level-3 lha fixture: %v", err)
	}

	dest := filepath.Join(tmp, "out")
	if err := extractLhaArchiveStoredOnly(lhaPath, "DIR3/FILE3.TXT", dest); err != nil {
		t.Fatalf("extractLhaArchiveStoredOnly level-3 failed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "file3.txt"))
	if err != nil {
		t.Fatalf("expected extracted level-3 file: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected extracted level-3 content: %q", got)
	}
}

func TestParseLhaEntriesSupportsLevel2DirAndCommentExtensions(t *testing.T) {
	tmp := t.TempDir()
	lhaPath := filepath.Join(tmp, "lv2-ext.lha")
	content := []byte("lv2-ext")
	if err := writeLhaLevel2StoredFixtureWithDirAndComment(lhaPath, "dirA/dirB", "file.txt", "hello-comment", content); err != nil {
		t.Fatalf("failed to create level-2 extension fixture: %v", err)
	}
	data, err := os.ReadFile(lhaPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := parseLhaEntries(data)
	if err != nil {
		t.Fatalf("parseLhaEntries level-2 extension failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one level-2 extension entry, got %d", len(entries))
	}
	entry := entries[0]
	if !strings.EqualFold(entry.Name, "dirA/dirB/file.txt") {
		t.Fatalf("unexpected level-2 extension entry name: %q", entry.Name)
	}
	if entry.Comment != "hello-comment" {
		t.Fatalf("unexpected level-2 extension comment: %q", entry.Comment)
	}
}

func TestExtractLhaStoredOnlySupportsLevel3DirExtension(t *testing.T) {
	tmp := t.TempDir()
	lhaPath := filepath.Join(tmp, "lv3-ext.lha")
	want := []byte("lv3-ext")
	if err := writeLhaLevel3StoredFixtureWithDir(lhaPath, "dirX/dirY", "file.bin", want); err != nil {
		t.Fatalf("failed to create level-3 extension fixture: %v", err)
	}
	dest := filepath.Join(tmp, "out")
	if err := extractLhaArchiveStoredOnly(lhaPath, "DIRX/DIRY/FILE.BIN", dest); err != nil {
		t.Fatalf("extract level-3 extension failed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "file.bin"))
	if err != nil {
		t.Fatalf("expected extracted level-3 extension file: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected extracted level-3 extension content: %q", got)
	}
}

func TestLhaJeromFallbackListAndExtract(t *testing.T) {
	lhaPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lha", "dirs-files.lha")

	entries, err := listLhaArchiveEntriesJerom(lhaPath)
	if err != nil {
		t.Fatalf("jerom list fallback failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected non-empty lha entries from jerom fallback")
	}
	found := false
	for _, entry := range entries {
		if strings.EqualFold(entry.Name, "dir1/file1.txt") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected entry dir1/file1.txt in jerom fallback list, got: %+v", entries)
	}
}

func writeLhaLevel1StoredFixture(path string, entryName string, content []byte) error {
	nameBytes := latin1Bytes(strings.ReplaceAll(entryName, "/", "\\"))
	if len(nameBytes) > 255 {
		return fmt.Errorf("entry name too long for level-1 fixture")
	}

	var b bytes.Buffer
	headerSize := 25 + len(nameBytes)
	b.WriteByte(byte(headerSize))
	b.WriteByte(0) // checksum is ignored by parser fallback and mirrors permissive legacy behavior
	b.WriteString("-lh0-")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(content)))
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(content)))
	_ = binary.Write(&b, binary.LittleEndian, uint32(0)) // DOS timestamp
	b.WriteByte(0x20)
	b.WriteByte(1) // level-1
	b.WriteByte(byte(len(nameBytes)))
	b.Write(nameBytes)
	_ = binary.Write(&b, binary.LittleEndian, crc16IBM(content))
	b.WriteByte('U') // Unix OSID
	_ = binary.Write(&b, binary.LittleEndian, uint16(0))
	b.Write(content)
	b.WriteByte(0)
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func writeLhaLevel2StoredFixture(path string, entryName string, content []byte) error {
	nameBytes := latin1Bytes(strings.ReplaceAll(entryName, "/", "\\"))
	extSize := 1 + len(nameBytes) + 2
	headerSize := 26 + extSize

	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, uint16(headerSize))
	b.WriteString("-lh0-")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(content)))
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(content)))
	_ = binary.Write(&b, binary.LittleEndian, uint32(0))
	b.WriteByte(0x20)
	b.WriteByte(2) // level-2
	_ = binary.Write(&b, binary.LittleEndian, crc16IBM(content))
	b.WriteByte('U')
	_ = binary.Write(&b, binary.LittleEndian, uint16(extSize))
	b.WriteByte(0x01) // filename extension header
	b.Write(nameBytes)
	_ = binary.Write(&b, binary.LittleEndian, uint16(0))
	b.Write(content)
	b.WriteByte(0)
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func writeLhaLevel2StoredFixtureWithDirAndComment(path string, dir string, base string, comment string, content []byte) error {
	nameBytes := latin1Bytes(base)
	dirBytes := latin1Bytes(strings.ReplaceAll(dir, "/", "\\"))
	commentBytes := latin1Bytes(comment)

	ext1Size := 1 + len(nameBytes) + 2
	ext2Size := 1 + len(dirBytes) + 2
	ext3Size := 1 + len(commentBytes) + 2
	headerSize := 26 + ext1Size + ext2Size + ext3Size

	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, uint16(headerSize))
	b.WriteString("-lh0-")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(content)))
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(content)))
	_ = binary.Write(&b, binary.LittleEndian, uint32(0))
	b.WriteByte(0x20)
	b.WriteByte(2)
	_ = binary.Write(&b, binary.LittleEndian, crc16IBM(content))
	b.WriteByte('U')
	_ = binary.Write(&b, binary.LittleEndian, uint16(ext1Size))

	b.WriteByte(0x01)
	b.Write(nameBytes)
	_ = binary.Write(&b, binary.LittleEndian, uint16(ext2Size))

	b.WriteByte(0x02)
	b.Write(dirBytes)
	_ = binary.Write(&b, binary.LittleEndian, uint16(ext3Size))

	b.WriteByte(0x3f)
	b.Write(commentBytes)
	_ = binary.Write(&b, binary.LittleEndian, uint16(0))

	b.Write(content)
	b.WriteByte(0)
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func writeLhaLevel3StoredFixture(path string, entryName string, content []byte) error {
	nameBytes := latin1Bytes(strings.ReplaceAll(entryName, "/", "\\"))
	extSize := 1 + len(nameBytes) + 4
	headerSize := 32 + extSize

	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, uint16(4)) // size-field length
	b.WriteString("-lh0-")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(content)))
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(content)))
	_ = binary.Write(&b, binary.LittleEndian, uint32(0))
	b.WriteByte(0x20)
	b.WriteByte(3) // level-3
	_ = binary.Write(&b, binary.LittleEndian, crc16IBM(content))
	b.WriteByte('U')
	_ = binary.Write(&b, binary.LittleEndian, uint32(headerSize))
	_ = binary.Write(&b, binary.LittleEndian, uint32(extSize))
	b.WriteByte(0x01) // filename extension header
	b.Write(nameBytes)
	_ = binary.Write(&b, binary.LittleEndian, uint32(0))
	b.Write(content)
	b.WriteByte(0)
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func writeLhaLevel3StoredFixtureWithDir(path string, dir string, base string, content []byte) error {
	nameBytes := latin1Bytes(base)
	dirBytes := latin1Bytes(strings.ReplaceAll(dir, "/", "\\"))
	ext1Size := 1 + len(nameBytes) + 4
	ext2Size := 1 + len(dirBytes) + 4
	headerSize := 32 + ext1Size + ext2Size

	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, uint16(4))
	b.WriteString("-lh0-")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(content)))
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(content)))
	_ = binary.Write(&b, binary.LittleEndian, uint32(0))
	b.WriteByte(0x20)
	b.WriteByte(3)
	_ = binary.Write(&b, binary.LittleEndian, crc16IBM(content))
	b.WriteByte('U')
	_ = binary.Write(&b, binary.LittleEndian, uint32(headerSize))
	_ = binary.Write(&b, binary.LittleEndian, uint32(ext1Size))

	b.WriteByte(0x01)
	b.Write(nameBytes)
	_ = binary.Write(&b, binary.LittleEndian, uint32(ext2Size))

	b.WriteByte(0x02)
	b.Write(dirBytes)
	_ = binary.Write(&b, binary.LittleEndian, uint32(0))

	b.Write(content)
	b.WriteByte(0)
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func crc16IBM(data []byte) uint16 {
	var crc uint16
	for _, by := range data {
		crc ^= uint16(by)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

type testLzxEntry struct {
	Name       string
	Data       []byte
	PackedData []byte
	Flags      byte
	IsDir      bool
	Comment    string
	PackMode   byte
}

func writeTestLzxArchive(path string, entries []testLzxEntry) error {
	var b bytes.Buffer
	info := make([]byte, lzxInfoHeaderSize)
	copy(info, []byte("LZX"))
	b.Write(info)

	for _, entry := range entries {
		name := entry.Name
		if entry.IsDir && !strings.HasSuffix(name, "/") {
			name += "/"
		}
		nameBytes := []byte(name)
		if len(nameBytes) > 255 {
			return fmt.Errorf("lzx test entry name too long: %q", entry.Name)
		}
		commentBytes := []byte(entry.Comment)
		if len(commentBytes) > 255 {
			return fmt.Errorf("lzx test entry comment too long for %q", entry.Name)
		}

		unpackedSize := len(entry.Data)
		if entry.IsDir {
			unpackedSize = 0
		}
		packedSize := len(entry.PackedData)
		if entry.IsDir {
			packedSize = 0
		}

		packMode := entry.PackMode
		if packMode == 0 {
			packMode = lzxPackModeStore
		}

		header := make([]byte, lzxArchiveHeaderSize)
		header[0] = 0x0f
		binary.LittleEndian.PutUint32(header[2:6], uint32(unpackedSize))
		binary.LittleEndian.PutUint32(header[6:10], uint32(packedSize))
		header[11] = packMode
		header[12] = entry.Flags
		header[14] = byte(len(commentBytes))
		header[30] = byte(len(nameBytes))

		headerHashInput := append([]byte(nil), header...)
		sum := crc32.ChecksumIEEE(headerHashInput)
		sum = crc32.Update(sum, crc32.IEEETable, nameBytes)
		sum = crc32.Update(sum, crc32.IEEETable, commentBytes)
		binary.LittleEndian.PutUint32(header[26:30], sum)

		b.Write(header)
		b.Write(nameBytes)
		b.Write(commentBytes)
		if packedSize > 0 {
			b.Write(entry.PackedData)
		}
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func TestArchiveListLzxStoredWorksWithoutBsdtar(t *testing.T) {
	t.Setenv("PATH", "")
	lzxPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lzx", "dirs-files.lzx")
	var out bytes.Buffer
	if err := run([]string{"archive", "list", lzxPath}, &out); err != nil {
		t.Fatalf("archive list lzx without bsdtar failed: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "dir1/dir3/") || !strings.Contains(got, "dir1/file1.txt") || !strings.Contains(got, "dir2/") {
		t.Fatalf("unexpected lzx list output: %q", got)
	}
}

func TestFsExtractLzxStoredWorksWithoutBsdtar(t *testing.T) {
	t.Setenv("PATH", "")
	tmp := t.TempDir()
	lzxPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lzx", "dirs-files.lzx")
	dest := filepath.Join(tmp, "out")
	var out bytes.Buffer
	if err := run([]string{"fs", "extract", lzxPath, dest, "--recursive"}, &out); err != nil {
		t.Fatalf("fs extract lzx stored without bsdtar failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "dir1", "dir3")); err != nil {
		t.Fatalf("expected extracted directory dir1/dir3: %v", err)
	}
	info, err := os.Stat(filepath.Join(dest, "dir1", "file1.txt"))
	if err != nil {
		t.Fatalf("expected extracted file dir1/file1.txt: %v", err)
	}
	if info.Size() != 1 {
		t.Fatalf("expected file1.txt size 1, got %d", info.Size())
	}
	if _, err := os.Stat(filepath.Join(dest, "dir2")); err != nil {
		t.Fatalf("expected extracted directory dir2: %v", err)
	}
}

func TestArchiveListLzxCompressedWorksWithoutBsdtar(t *testing.T) {
	t.Setenv("PATH", "")
	lzxPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lzx", "amiga.lzx")
	var out bytes.Buffer
	if err := run([]string{"archive", "list", lzxPath}, &out); err != nil {
		t.Fatalf("archive list lzx compressed without bsdtar failed: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "test1.info") || !strings.Contains(got, "test1/test2.info") {
		t.Fatalf("unexpected compressed lzx list output: %q", got)
	}
}

func TestFsExtractLzxCompressedWorksWithoutBsdtar(t *testing.T) {
	t.Setenv("PATH", "")
	tmp := t.TempDir()
	lzxPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lzx", "amiga.lzx")
	dest := filepath.Join(tmp, "out")
	var out bytes.Buffer
	if err := run([]string{"fs", "extract", lzxPath, dest, "--recursive"}, &out); err != nil {
		t.Fatalf("fs extract lzx compressed without bsdtar failed: %v", err)
	}
	info, err := os.Stat(filepath.Join(dest, "test1.info"))
	if err != nil {
		t.Fatalf("expected extracted file test1.info: %v", err)
	}
	if info.Size() != 900 {
		t.Fatalf("expected test1.info size 900, got %d", info.Size())
	}
	info, err = os.Stat(filepath.Join(dest, "test1", "test2.info"))
	if err != nil {
		t.Fatalf("expected extracted file test1/test2.info: %v", err)
	}
	if info.Size() != 900 {
		t.Fatalf("expected test1/test2.info size 900, got %d", info.Size())
	}
}

func TestExtractLzxMergedGroupSupportsAnchorFirstStored(t *testing.T) {
	t.Setenv("PATH", "")
	tmp := t.TempDir()
	lzxPath := filepath.Join(tmp, "merged-anchor-first.lzx")
	file1 := []byte("first")
	file2 := []byte("second")
	merged := append(append([]byte{}, file1...), file2...)
	if err := writeTestLzxArchive(lzxPath, []testLzxEntry{
		{
			Name:       "a.bin",
			Data:       file1,
			PackedData: merged,
			Flags:      lzxMergedFlag,
		},
		{
			Name:  "b.bin",
			Data:  file2,
			Flags: lzxMergedFlag,
		},
	}); err != nil {
		t.Fatalf("failed to write synthetic lzx archive: %v", err)
	}

	dest := filepath.Join(tmp, "out")
	var out bytes.Buffer
	if err := run([]string{"fs", "extract", lzxPath, dest, "--recursive"}, &out); err != nil {
		t.Fatalf("fs extract synthetic merged lzx failed: %v", err)
	}

	gotA, err := os.ReadFile(filepath.Join(dest, "a.bin"))
	if err != nil {
		t.Fatalf("expected extracted file a.bin: %v", err)
	}
	if !bytes.Equal(gotA, file1) {
		t.Fatalf("unexpected extracted a.bin content: %q", gotA)
	}
	gotB, err := os.ReadFile(filepath.Join(dest, "b.bin"))
	if err != nil {
		t.Fatalf("expected extracted file b.bin: %v", err)
	}
	if !bytes.Equal(gotB, file2) {
		t.Fatalf("unexpected extracted b.bin content: %q", gotB)
	}
}

func TestExtractLzxStoredAllowsTrailingZeroPadding(t *testing.T) {
	t.Setenv("PATH", "")
	tmp := t.TempDir()
	lzxPath := filepath.Join(tmp, "stored-trailing-padding.lzx")
	content := []byte("hello")
	padded := append(append([]byte{}, content...), 0, 0, 0, 0)
	if err := writeTestLzxArchive(lzxPath, []testLzxEntry{
		{
			Name:       "file.txt",
			Data:       content,
			PackedData: padded,
		},
	}); err != nil {
		t.Fatalf("failed to write synthetic padded lzx archive: %v", err)
	}

	dest := filepath.Join(tmp, "out")
	var out bytes.Buffer
	if err := run([]string{"fs", "extract", lzxPath, dest, "--recursive"}, &out); err != nil {
		t.Fatalf("fs extract synthetic padded lzx failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "file.txt"))
	if err != nil {
		t.Fatalf("expected extracted file.txt: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("unexpected extracted content: %q", got)
	}
}

func TestArchiveListRarWorksWithoutBsdtar(t *testing.T) {
	t.Setenv("PATH", "")
	rarPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "compressed-images", "1gb.img.rar")
	var out bytes.Buffer
	if err := run([]string{"archive", "list", rarPath}, &out); err != nil {
		t.Fatalf("archive list rar without bsdtar failed: %v", err)
	}
	got := strings.TrimSpace(out.String())
	if got == "" || !strings.Contains(strings.ToLower(got), "1gb") {
		t.Fatalf("unexpected rar list output: %q", got)
	}
}

func TestResolveRarPrimaryVolumePathUsesRarForOldStyleVolume(t *testing.T) {
	tmp := t.TempDir()
	rarPath := filepath.Join(tmp, "archive.rar")
	r00Path := filepath.Join(tmp, "archive.r00")
	if err := os.WriteFile(rarPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r00Path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	got := resolveRarPrimaryVolumePath(r00Path)
	if got != rarPath {
		t.Fatalf("expected %q, got %q", rarPath, got)
	}
}

func TestResolveRarPrimaryVolumePathUsesPart1ForPartVolumes(t *testing.T) {
	tmp := t.TempDir()
	part1 := filepath.Join(tmp, "archive.part1.rar")
	part2 := filepath.Join(tmp, "archive.part2.rar")
	if err := os.WriteFile(part1, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(part2, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	got := resolveRarPrimaryVolumePath(part2)
	if got != part1 {
		t.Fatalf("expected %q, got %q", part1, got)
	}
}

func TestRarPasswordFromEnvPrefersImagerVar(t *testing.T) {
	t.Setenv("HST_IMAGER_RAR_PASSWORD", "pw-a")
	t.Setenv("HST_RAR_PASSWORD", "pw-b")
	if got := rarPasswordFromEnv(); got != "pw-a" {
		t.Fatalf("expected HST_IMAGER_RAR_PASSWORD value, got %q", got)
	}
}

func TestOpenSourceReaderRarMatchesZipPrefix(t *testing.T) {
	rarPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "compressed-images", "1gb.img.rar")
	zipPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "compressed-images", "1gb.img.zip")

	rarReader, err := openSourceReader(rarPath)
	if err != nil {
		t.Fatalf("openSourceReader rar failed: %v", err)
	}
	defer rarReader.Close()

	zipReader, err := openSourceReader(zipPath)
	if err != nil {
		t.Fatalf("openSourceReader zip failed: %v", err)
	}
	defer zipReader.Close()

	rarPrefix := make([]byte, 8192)
	nRar, err := io.ReadFull(rarReader, rarPrefix)
	if err != nil {
		t.Fatalf("read rar prefix failed: %v", err)
	}
	zipPrefix := make([]byte, 8192)
	nZip, err := io.ReadFull(zipReader, zipPrefix)
	if err != nil {
		t.Fatalf("read zip prefix failed: %v", err)
	}
	if nRar != nZip {
		t.Fatalf("prefix sizes differ, rar=%d zip=%d", nRar, nZip)
	}
	if !bytes.Equal(rarPrefix[:nRar], zipPrefix[:nZip]) {
		t.Fatal("rar and zip decoded prefixes differ")
	}
}

func TestFsDirJsonSingleStreamXzReturnsEmptyEntries(t *testing.T) {
	xzPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Xz", "test.txt.xz")
	var out bytes.Buffer
	if err := run([]string{"--format", "json", "fs", "dir", xzPath}, &out); err != nil {
		t.Fatalf("fs dir json xz failed: %v", err)
	}
	var payload struct {
		Entries []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json output: %v", err)
	}
	if len(payload.Entries) != 0 {
		t.Fatalf("expected no entries for xz single-stream source, got %d", len(payload.Entries))
	}
}

func TestFsDirJsonSingleStreamLzwReturnsEmptyEntries(t *testing.T) {
	lzwPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lzw", "test.txt.Z")
	var out bytes.Buffer
	if err := run([]string{"--format", "json", "fs", "dir", lzwPath}, &out); err != nil {
		t.Fatalf("fs dir json lzw failed: %v", err)
	}
	var payload struct {
		Entries []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json output: %v", err)
	}
	if len(payload.Entries) != 0 {
		t.Fatalf("expected no entries for lzw single-stream source, got %d", len(payload.Entries))
	}
}

func TestWriteFromXzCompressedSource(t *testing.T) {
	xzPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Xz", "test.txt.xz")
	dest := filepath.Join(t.TempDir(), "out.txt")
	src, err := openSourceReader(xzPath)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	want, err := io.ReadAll(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, make([]byte, len(want)), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"write", xzPath, dest}, &out); err != nil {
		t.Fatalf("write from xz source failed: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(normalizeLineEndings(got), normalizeLineEndings(want)) {
		t.Fatal("written data does not match uncompressed xz content")
	}
}

func TestWriteFromLzwCompressedSource(t *testing.T) {
	lzwPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lzw", "test.txt.Z")
	dest := filepath.Join(t.TempDir(), "out.txt")
	src, err := openSourceReader(lzwPath)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	want, err := io.ReadAll(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, make([]byte, len(want)), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"write", lzwPath, dest}, &out); err != nil {
		t.Fatalf("write from lzw source failed: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(normalizeLineEndings(got), normalizeLineEndings(want)) {
		t.Fatal("written data does not match uncompressed lzw content")
	}
}

func TestWriteFailsWhenSourceExceedsDestinationSize(t *testing.T) {
	xzPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Xz", "test.txt.xz")
	dest := filepath.Join(t.TempDir(), "small.bin")
	if err := os.WriteFile(dest, make([]byte, 32), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := run([]string{"write", xzPath, dest}, &out)
	if err == nil {
		t.Fatal("expected write to fail when destination is smaller than source")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "destination partition too small") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestXzRoundTripViaSourceAndDestinationWriters(t *testing.T) {
	tmp := t.TempDir()
	xzPath := filepath.Join(tmp, "roundtrip.img.xz")
	source := bytes.Repeat([]byte{0x5a, 0xa5, 0x11, 0xee}, 1024)

	w, err := openDestinationWriter(xzPath)
	if err != nil {
		t.Fatalf("openDestinationWriter xz failed: %v", err)
	}
	if _, err := w.Write(source); err != nil {
		t.Fatalf("write xz failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close xz writer failed: %v", err)
	}

	r, err := openSourceReader(xzPath)
	if err != nil {
		t.Fatalf("openSourceReader xz failed: %v", err)
	}
	defer r.Close()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read xz failed: %v", err)
	}
	if !bytes.Equal(got, source) {
		t.Fatal("xz roundtrip data mismatch")
	}
}

func writeTarGzArchive(path string, files map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}); err != nil {
			return err
		}
		if _, err := io.Copy(tw, strings.NewReader(content)); err != nil {
			return err
		}
	}
	return nil
}

func writeTarXzArchive(path string, files map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	xw, err := xz.NewWriter(f)
	if err != nil {
		return err
	}
	defer xw.Close()
	tw := tar.NewWriter(xw)
	defer tw.Close()
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}); err != nil {
			return err
		}
		if _, err := io.Copy(tw, strings.NewReader(content)); err != nil {
			return err
		}
	}
	return nil
}

func writeGzipFileWithName(path, name string, payload []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	gw.Name = name
	if _, err := gw.Write(payload); err != nil {
		_ = gw.Close()
		return err
	}
	return gw.Close()
}

func writeBzip2FileWithSystemTool(t *testing.T, path string, payload []byte) error {
	t.Helper()
	if _, err := exec.LookPath("bzip2"); err != nil {
		t.Skipf("bzip2 tool not available: %v", err)
	}
	cmd := exec.Command("bzip2", "-c")
	cmd.Stdin = bytes.NewReader(payload)
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func normalizeLineEndings(data []byte) []byte {
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
}

func splitRecordedArgsLines(data []byte) []string {
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(strings.TrimSpace(normalized), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines
}

func writeLegacyBridgeStub(t *testing.T, dir, argsFile, marker string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "legacy.cmd")
		script := strings.Join([]string{
			"@echo off",
			"setlocal EnableExtensions",
			fmt.Sprintf("if exist \"%s\" del \"%s\"", argsFile, argsFile),
			fmt.Sprintf("for %%%%A in (%%*) do >> \"%s\" echo %%%%~A", argsFile),
			fmt.Sprintf("echo %s", marker),
			"",
		}, "\r\n")
		if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	path := filepath.Join(dir, "legacy.sh")
	script := fmt.Sprintf("#!/bin/sh\nprintf \"%%s\\n\" \"$@\" > %q\necho %s\n", argsFile, marker)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
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

func TestTransferSupportsStartOffsetsAndVerify(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.bin")
	dst := filepath.Join(tmp, "dst.bin")
	if err := os.WriteFile(src, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"transfer", src, dst, "--size", "4", "--src-start", "2", "--dest-start", "5", "--verify"}, &out); err != nil {
		t.Fatalf("transfer with offsets and verify failed: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	want := append(make([]byte, 5), []byte("2345")...)
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected transfer output bytes: got=%v want=%v", got, want)
	}
}

func TestTransferSupportsSourceDestinationAliasOffsets(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.bin")
	dst := filepath.Join(tmp, "dst.bin")
	if err := os.WriteFile(src, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"transfer", src, dst, "--size", "4", "--source-start", "2", "--destination-start", "5"}, &out); err != nil {
		t.Fatalf("transfer with source/destination aliases failed: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	want := append(make([]byte, 5), []byte("2345")...)
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected transfer output bytes: got=%v want=%v", got, want)
	}
}

func TestReadWriteStartAlias(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.bin")
	dst := filepath.Join(tmp, "dst.bin")
	outFile := filepath.Join(tmp, "out.bin")
	if err := os.WriteFile(src, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("xxxxxxxxxx"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"write", src, dst, "--size", "3", "--start", "4"}, &out); err != nil {
		t.Fatalf("write with --start failed: %v", err)
	}
	gotDst, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	wantDst := []byte("xxxx012xxx")
	if !bytes.Equal(gotDst, wantDst) {
		t.Fatalf("unexpected destination content after write --start: got=%q want=%q", gotDst, wantDst)
	}

	out.Reset()
	if err := run([]string{"read", dst, outFile, "--size", "3", "--start", "4"}, &out); err != nil {
		t.Fatalf("read with --start failed: %v", err)
	}
	gotOut, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotOut, []byte("012")) {
		t.Fatalf("unexpected output after read --start: %q", gotOut)
	}
}

func TestReadWriteStartShortAlias(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.bin")
	dst := filepath.Join(tmp, "dst.bin")
	outFile := filepath.Join(tmp, "out.bin")
	if err := os.WriteFile(src, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("xxxxxxxxxx"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"write", src, dst, "--size", "3", "-st", "4"}, &out); err != nil {
		t.Fatalf("write with -st failed: %v", err)
	}
	gotDst, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	wantDst := []byte("xxxx012xxx")
	if !bytes.Equal(gotDst, wantDst) {
		t.Fatalf("unexpected destination content after write -st: got=%q want=%q", gotDst, wantDst)
	}

	out.Reset()
	if err := run([]string{"read", dst, outFile, "--size", "3", "-st", "4"}, &out); err != nil {
		t.Fatalf("read with -st failed: %v", err)
	}
	gotOut, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotOut, []byte("012")) {
		t.Fatalf("unexpected output after read -st: %q", gotOut)
	}
}

func TestCompareSupportsOffsets(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.bin")
	dst := filepath.Join(tmp, "dst.bin")
	if err := os.WriteFile(src, []byte("ABCDzz"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("xxABCDyy"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"compare", src, dst, "--size", "4", "--source-start", "0", "--destination-start", "2"}, &out); err != nil {
		t.Fatalf("compare with offsets failed: %v", err)
	}
}

func TestCompareSupportsTransferStyleOffsetAliases(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.bin")
	dst := filepath.Join(tmp, "dst.bin")
	if err := os.WriteFile(src, []byte("ABCDzz"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("xxABCDyy"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"compare", src, dst, "--size", "4", "--src-start", "0", "--dest-start", "2"}, &out); err != nil {
		t.Fatalf("compare with transfer-style aliases failed: %v", err)
	}

	out.Reset()
	if err := run([]string{"compare", src, dst, "--size", "4", "-ss", "0", "-ds", "2"}, &out); err != nil {
		t.Fatalf("compare with short offset aliases failed: %v", err)
	}
}

func TestTransferAcceptsLegacyNoopFlags(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.bin")
	dst := filepath.Join(tmp, "dst.bin")
	if err := os.WriteFile(src, []byte("abcdefgh"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"transfer", src, dst, "--retries", "2", "--force", "--skip-unused-sectors", "false"}, &out); err != nil {
		t.Fatalf("transfer with legacy noop flags failed: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("abcdefgh")) {
		t.Fatalf("unexpected transfer output: %q", got)
	}
}

func TestTransferAcceptsVerifyBoolValue(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.bin")
	dst := filepath.Join(tmp, "dst.bin")
	if err := os.WriteFile(src, []byte("abcdefgh"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"transfer", src, dst, "--verify", "false"}, &out); err != nil {
		t.Fatalf("transfer with --verify false failed: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("abcdefgh")) {
		t.Fatalf("unexpected transfer output: %q", got)
	}
}

func TestCompareAcceptsLegacyNoopFlags(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.bin")
	dst := filepath.Join(tmp, "dst.bin")
	if err := os.WriteFile(src, []byte("abcdefgh"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("abcdefgh"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"compare", src, dst, "--retries", "3", "--force", "true", "--skip-unused-sectors"}, &out); err != nil {
		t.Fatalf("compare with legacy noop flags failed: %v", err)
	}
}

func TestTransferCompareAcceptShortFlags(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.bin")
	dst := filepath.Join(tmp, "dst.bin")
	if err := os.WriteFile(src, []byte("abcdefgh"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"transfer", src, dst, "--size", "8", "-r", "1", "-f", "-v"}, &out); err != nil {
		t.Fatalf("transfer with short flags failed: %v", err)
	}
	if err := run([]string{"compare", src, dst, "--size", "8", "-r", "1", "-f"}, &out); err != nil {
		t.Fatalf("compare with short flags failed: %v", err)
	}
}

func TestAdvancedCommandFamilies(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_CACHE_HOME", tmp)

	media := filepath.Join(tmp, "disk.img")
	fsBin := filepath.Join(tmp, "pfs3aio")
	if err := os.WriteFile(fsBin, []byte("filesystem-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"blank", media, "8MB"}, &out); err != nil {
		t.Fatalf("blank failed: %v", err)
	}

	out.Reset()
	if err := run([]string{"mbr", "init", media}, &out); err != nil {
		t.Fatalf("mbr init failed: %v", err)
	}
	if err := run([]string{"mbr", "part", "add", media, "NTFS", "1KB"}, &out); err != nil {
		t.Fatalf("mbr part add failed: %v", err)
	}
	if err := run([]string{"mbr", "part", "format", media, "1", "PC"}, &out); err != nil {
		t.Fatalf("mbr part format failed: %v", err)
	}
	out.Reset()
	if err := run([]string{"--format", "json", "mbr", "info", media}, &out); err != nil {
		t.Fatalf("mbr info failed: %v", err)
	}
	if !strings.Contains(out.String(), "\"parts\"") {
		t.Fatalf("unexpected mbr info output: %q", out.String())
	}

	out.Reset()
	if err := run([]string{"gpt", "init", media}, &out); err != nil {
		t.Fatalf("gpt init failed: %v", err)
	}
	if err := run([]string{"gpt", "part", "add", media, "LINUX", "2KB"}, &out); err != nil {
		t.Fatalf("gpt part add failed: %v", err)
	}
	out.Reset()
	if err := run([]string{"--format", "json", "gpt", "info", media}, &out); err != nil {
		t.Fatalf("gpt info failed: %v", err)
	}
	if !strings.Contains(out.String(), "\"parts\"") {
		t.Fatalf("unexpected gpt info output: %q", out.String())
	}

	out.Reset()
	if err := run([]string{"rdb", "init", media}, &out); err != nil {
		t.Fatalf("rdb init failed: %v", err)
	}
	if err := run([]string{"rdb", "fs", "add", media, fsBin, "PDS3"}, &out); err != nil {
		t.Fatalf("rdb fs add failed: %v", err)
	}
	if err := run([]string{"rdb", "part", "add", media, "DH0", "PDS3", "1KB"}, &out); err != nil {
		t.Fatalf("rdb part add failed: %v", err)
	}
	out.Reset()
	if err := run([]string{"--format", "json", "rdb", "info", media}, &out); err != nil {
		t.Fatalf("rdb info failed: %v", err)
	}
	if !strings.Contains(out.String(), "\"filesystems\"") || !strings.Contains(out.String(), "\"partitions\"") {
		t.Fatalf("unexpected rdb info output: %q", out.String())
	}
}

func TestFsBlockOptimizeAndAdfCommands(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)

	srcDir := filepath.Join(tmp, "src")
	dstDir := filepath.Join(tmp, "dst")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"fs", "mkdir", dstDir}, &out); err != nil {
		t.Fatalf("fs mkdir failed: %v", err)
	}
	if err := run([]string{"fs", "copy", srcDir, dstDir, "--recursive"}, &out); err != nil {
		t.Fatalf("fs copy failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstDir, "file.txt")); err != nil {
		t.Fatalf("expected copied file: %v", err)
	}

	image := filepath.Join(tmp, "image.bin")
	if err := os.WriteFile(image, append([]byte("abcd"), make([]byte, 128)...), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"optimize", image, "--size", "4"}, &out); err != nil {
		t.Fatalf("optimize failed: %v", err)
	}
	info, err := os.Stat(image)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 4 {
		t.Fatalf("expected optimized size 4, got %d", info.Size())
	}

	blockOut := filepath.Join(tmp, "block.bin")
	out.Reset()
	if err := run([]string{"block", "read", image, "0", "2", blockOut}, &out); err != nil {
		t.Fatalf("block read failed: %v", err)
	}
	b, err := os.ReadFile(blockOut)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "ab" {
		t.Fatalf("unexpected block read content: %q", string(b))
	}

	out.Reset()
	if err := run([]string{"block", "view", image, "0", "4"}, &out); err != nil {
		t.Fatalf("block view failed: %v", err)
	}
	if !strings.Contains(out.String(), "61 62 63 64") {
		t.Fatalf("unexpected block view output: %q", out.String())
	}

	adf := filepath.Join(tmp, "disk.adf")
	out.Reset()
	if err := run([]string{"adf", "create", adf}, &out); err != nil {
		t.Fatalf("adf create failed: %v", err)
	}
	adfInfo, err := os.Stat(adf)
	if err != nil {
		t.Fatal(err)
	}
	if adfInfo.Size() != 901120 {
		t.Fatalf("unexpected adf size: %d", adfInfo.Size())
	}
}

func TestBlockReadViewOptionSyntaxCompatibility(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "block.bin")
	outPath := filepath.Join(tmp, "slice.bin")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"block", "read", path, outPath, "--start", "2", "--end", "6", "--block-size", "2"}, &out); err != nil {
		t.Fatalf("block read option syntax failed: %v", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("2345")) {
		t.Fatalf("unexpected block read option output: %q", got)
	}

	out.Reset()
	if err := run([]string{"block", "view", path, "--start", "1", "--block-size", "4"}, &out); err != nil {
		t.Fatalf("block view option syntax failed: %v", err)
	}
	if !strings.Contains(out.String(), "31 32 33 34") {
		t.Fatalf("unexpected block view option output: %q", out.String())
	}
}

func TestOptimizeWithoutPartitionTableFails(t *testing.T) {
	tmp := t.TempDir()
	image := filepath.Join(tmp, "plain.bin")
	if err := os.WriteFile(image, append([]byte("abcd"), make([]byte, 128)...), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := run([]string{"optimize", image}, &out)
	if err == nil {
		t.Fatal("expected optimize to fail when no partition table is present")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "no partition table") {
		t.Fatalf("unexpected optimize error: %v", err)
	}
}

func TestOptimizeAutoUsesMbrSize(t *testing.T) {
	tmp := t.TempDir()
	media := filepath.Join(tmp, "disk.img")
	var out bytes.Buffer
	if err := run([]string{"blank", media, "16MB"}, &out); err != nil {
		t.Fatalf("blank failed: %v", err)
	}
	if err := run([]string{"mbr", "initialize", media}, &out); err != nil {
		t.Fatalf("mbr initialize failed: %v", err)
	}
	if err := run([]string{"mbr", "part", "add", media, "fat32", "2MB"}, &out); err != nil {
		t.Fatalf("mbr part add failed: %v", err)
	}

	parts, err := readMbrPartitions(media)
	if err != nil {
		t.Fatalf("read mbr partitions failed: %v", err)
	}
	want := estimateMbrOptimizeSize(parts)
	if want <= 0 {
		t.Fatalf("expected positive mbr optimize target, got %d", want)
	}

	out.Reset()
	if err := run([]string{"optimize", media}, &out); err != nil {
		t.Fatalf("optimize failed: %v", err)
	}
	info, err := os.Stat(media)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != want {
		t.Fatalf("expected optimize size %d, got %d", want, info.Size())
	}
}

func TestOptimizePartitionTableOptionRdb(t *testing.T) {
	tmp := t.TempDir()
	media := filepath.Join(tmp, "rdb.img")
	var out bytes.Buffer
	if err := run([]string{"blank", media, "16MB"}, &out); err != nil {
		t.Fatalf("blank failed: %v", err)
	}
	if err := run([]string{"rdb", "initialize", media}, &out); err != nil {
		t.Fatalf("rdb initialize failed: %v", err)
	}
	if err := run([]string{"rdb", "resize", media, "3MB"}, &out); err != nil {
		t.Fatalf("rdb resize failed: %v", err)
	}

	out.Reset()
	if err := run([]string{"optimize", media, "--partition-table", "rdb"}, &out); err != nil {
		t.Fatalf("optimize rdb failed: %v", err)
	}
	info, err := os.Stat(media)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 3*1024*1024 {
		t.Fatalf("expected optimize size %d, got %d", 3*1024*1024, info.Size())
	}
}

func TestFormatRootMbrWorkflow(t *testing.T) {
	tmp := t.TempDir()
	media := filepath.Join(tmp, "mbr-format.img")
	var out bytes.Buffer
	if err := run([]string{"blank", media, "16MB"}, &out); err != nil {
		t.Fatalf("blank failed: %v", err)
	}
	if err := run([]string{"format", media, "mbr", "ntfs", "--size", "2MB"}, &out); err != nil {
		t.Fatalf("format mbr failed: %v", err)
	}

	out.Reset()
	if err := run([]string{"--format", "json", "mbr", "info", media}, &out); err != nil {
		t.Fatalf("mbr info failed: %v", err)
	}
	var result struct {
		Parts []Part `json:"parts"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal mbr info failed: %v", err)
	}
	if len(result.Parts) != 1 {
		t.Fatalf("expected one mbr partition, got %d", len(result.Parts))
	}
	if result.Parts[0].Size != 2*1024*1024 {
		t.Fatalf("expected mbr partition size %d, got %d", 2*1024*1024, result.Parts[0].Size)
	}
}

func TestFormatRootGptWorkflow(t *testing.T) {
	tmp := t.TempDir()
	media := filepath.Join(tmp, "gpt-format.img")
	var out bytes.Buffer
	if err := run([]string{"blank", media, "16MB"}, &out); err != nil {
		t.Fatalf("blank failed: %v", err)
	}
	if err := run([]string{"format", media, "gpt", "ntfs", "--size", "2MB"}, &out); err != nil {
		t.Fatalf("format gpt failed: %v", err)
	}

	out.Reset()
	if err := run([]string{"--format", "json", "gpt", "info", media}, &out); err != nil {
		t.Fatalf("gpt info failed: %v", err)
	}
	var result struct {
		Parts []Part `json:"parts"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal gpt info failed: %v", err)
	}
	if len(result.Parts) != 1 {
		t.Fatalf("expected one gpt partition, got %d", len(result.Parts))
	}
	if result.Parts[0].Size != 2*1024*1024 {
		t.Fatalf("expected gpt partition size %d, got %d", 2*1024*1024, result.Parts[0].Size)
	}
}

func TestFormatRootRdbWorkflow(t *testing.T) {
	tmp := t.TempDir()
	media := filepath.Join(tmp, "rdb-format.img")
	fsBin := filepath.Join(tmp, "pfs3aio")
	var out bytes.Buffer
	if err := os.WriteFile(fsBin, []byte("filesystem-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"blank", media, "16MB"}, &out); err != nil {
		t.Fatalf("blank failed: %v", err)
	}
	if err := run([]string{"format", media, "rdb", "pds3", "--size", "4MB", "--max-partition-size", "2MB", "--file-system-path", fsBin}, &out); err != nil {
		t.Fatalf("format rdb failed: %v", err)
	}

	out.Reset()
	if err := run([]string{"--format", "json", "rdb", "info", media}, &out); err != nil {
		t.Fatalf("rdb info failed: %v", err)
	}
	var result struct {
		RdbSize     int64           `json:"rdbSize"`
		Partitions  []Part          `json:"partitions"`
		Filesystems []RdbFileSystem `json:"filesystems"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal rdb info failed: %v", err)
	}
	if result.RdbSize != 4*1024*1024 {
		t.Fatalf("expected rdb size %d, got %d", 4*1024*1024, result.RdbSize)
	}
	if len(result.Partitions) == 0 {
		t.Fatal("expected at least one rdb partition")
	}
	if len(result.Filesystems) == 0 {
		t.Fatal("expected at least one rdb filesystem")
	}
}

func TestFormatRootPiStormWorkflow(t *testing.T) {
	tmp := t.TempDir()
	media := filepath.Join(tmp, "pistorm-format.img")
	fsBin := filepath.Join(tmp, "pfs3aio")
	var out bytes.Buffer
	if err := os.WriteFile(fsBin, []byte("filesystem-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"blank", media, "3GB"}, &out); err != nil {
		t.Fatalf("blank failed: %v", err)
	}
	if err := run([]string{"format", media, "pistorm", "pds3", "--max-partition-size", "1GB", "--file-system-path", fsBin}, &out); err != nil {
		t.Fatalf("format pistorm failed: %v", err)
	}

	parts, err := readMbrPartitions(media)
	if err != nil {
		t.Fatalf("read mbr partitions failed: %v", err)
	}
	if len(parts) < 2 {
		t.Fatalf("expected at least 2 mbr partitions, got %d", len(parts))
	}
	p1, err := findMbrPart(parts, 1)
	if err != nil {
		t.Fatalf("expected mbr part 1: %v", err)
	}
	if p1.TypeCode != 0x0c {
		t.Fatalf("expected mbr part 1 type 0x0c, got 0x%02x", p1.TypeCode)
	}
	p2, err := findMbrPart(parts, 2)
	if err != nil {
		t.Fatalf("expected mbr part 2: %v", err)
	}
	if p2.TypeCode != 0x76 {
		t.Fatalf("expected mbr part 2 type 0x76, got 0x%02x", p2.TypeCode)
	}

	sig, err := readBytesAt(media, int64(p2.StartLBA)*mbrSectorSize, 4)
	if err != nil {
		t.Fatalf("read piStorm rdb partition signature failed: %v", err)
	}
	if string(sig) != "RDSK" {
		t.Fatalf("expected RDSK signature in mbr part 2, got %q", string(sig))
	}
}

func TestGptPartAddSupportsLegacyNameArgumentOrder(t *testing.T) {
	tmp := t.TempDir()
	media := filepath.Join(tmp, "gpt-legacy-args.img")

	var out bytes.Buffer
	if err := run([]string{"blank", media, "16MB"}, &out); err != nil {
		t.Fatalf("blank failed: %v", err)
	}
	out.Reset()
	if err := run([]string{"gpt", "initialize", media}, &out); err != nil {
		t.Fatalf("gpt initialize failed: %v", err)
	}
	out.Reset()
	if err := run([]string{"gpt", "part", "add", media, "linux", "DATA", "1MB"}, &out); err != nil {
		t.Fatalf("gpt part add legacy syntax failed: %v", err)
	}

	out.Reset()
	if err := run([]string{"--format", "json", "gpt", "info", media}, &out); err != nil {
		t.Fatalf("gpt info failed: %v", err)
	}
	if !strings.Contains(out.String(), "\"name\": \"DATA\"") {
		t.Fatalf("expected GPT partition name DATA in info output, got: %q", out.String())
	}
}

func TestMbrPartFormatFat32RequiresMinimumSectors(t *testing.T) {
	tmp := t.TempDir()
	media := filepath.Join(tmp, "mbr-fat32-min.img")
	var out bytes.Buffer

	if err := run([]string{"blank", media, "32MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"mbr", "initialize", media}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"mbr", "part", "add", media, "fat32", "4MB"}, &out); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"mbr", "part", "format", media, "1", "PC"}, &out)
	if err == nil {
		t.Fatal("expected mbr part format to fail for too-small FAT32 partition")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "fat32 requires a minimum") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGptPartFormatSupportsLegacyTypeAndName(t *testing.T) {
	tmp := t.TempDir()
	media := filepath.Join(tmp, "gpt-format-legacy.img")
	var out bytes.Buffer

	if err := run([]string{"blank", media, "64MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"gpt", "initialize", media}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"gpt", "part", "add", media, "ntfs", "DATA", "32MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"gpt", "part", "format", media, "1", "ntfs", "VOL"}, &out); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := run([]string{"--format", "json", "gpt", "info", media}, &out); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Parts []Part `json:"parts"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Parts) != 1 {
		t.Fatalf("expected one GPT partition, got %d", len(payload.Parts))
	}
	if payload.Parts[0].Name != "DATA" {
		t.Fatalf("expected GPT partition name to remain DATA, got %q", payload.Parts[0].Name)
	}
}

func TestMbrLowLevelBinaryParity(t *testing.T) {
	tmp := t.TempDir()
	mediaA := filepath.Join(tmp, "disk-a.img")
	mediaB := filepath.Join(tmp, "disk-b.img")
	exportPath := filepath.Join(tmp, "part.bin")

	var out bytes.Buffer
	if err := run([]string{"blank", mediaA, "1MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"blank", mediaB, "1MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"mbr", "init", mediaA}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"mbr", "init", mediaB}, &out); err != nil {
		t.Fatal(err)
	}

	sector0, err := os.ReadFile(mediaA)
	if err != nil {
		t.Fatal(err)
	}
	if sector0[510] != 0x55 || sector0[511] != 0xaa {
		t.Fatalf("missing mbr signature: %x %x", sector0[510], sector0[511])
	}

	if err := run([]string{"mbr", "part", "add", mediaA, "fat32", "16KB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"mbr", "part", "add", mediaB, "fat32", "16KB"}, &out); err != nil {
		t.Fatal(err)
	}
	sector0, err = os.ReadFile(mediaA)
	if err != nil {
		t.Fatal(err)
	}
	entry := sector0[446 : 446+16]
	totalSectors, err := mediaCapacitySectors(mediaA)
	if err != nil {
		t.Fatal(err)
	}
	startHead, startSecCyl, startCyl := encodeMbrChs(63, totalSectors)
	endHead, endSecCyl, endCyl := encodeMbrChs(63+uint32((16*1024)/512)-1, totalSectors)
	if entry[1] != startHead || entry[2] != startSecCyl || entry[3] != startCyl {
		t.Fatalf("unexpected start CHS bytes: %02x %02x %02x", entry[1], entry[2], entry[3])
	}
	if entry[5] != endHead || entry[6] != endSecCyl || entry[7] != endCyl {
		t.Fatalf("unexpected end CHS bytes: %02x %02x %02x", entry[5], entry[6], entry[7])
	}

	out.Reset()
	if err := run([]string{"--format", "json", "mbr", "info", mediaA}, &out); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Parts []Part `json:"parts"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Parts) != 1 {
		t.Fatalf("expected 1 mbr partition, got %d", len(payload.Parts))
	}
	if payload.Parts[0].Start != 63*512 {
		t.Fatalf("unexpected start offset: %d", payload.Parts[0].Start)
	}
	if payload.Parts[0].Size != 16*1024 {
		t.Fatalf("unexpected partition size: %d", payload.Parts[0].Size)
	}

	// Write pattern directly at MBR partition start.
	pattern := bytes.Repeat([]byte{0xab}, 16*1024)
	fa, err := os.OpenFile(mediaA, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fa.WriteAt(pattern, int64(payload.Parts[0].Start)); err != nil {
		t.Fatal(err)
	}
	_ = fa.Close()

	if err := run([]string{"mbr", "part", "export", mediaA, "1", exportPath}, &out); err != nil {
		t.Fatal(err)
	}
	exported, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(exported, pattern) {
		t.Fatal("exported bytes do not match source partition bytes")
	}

	zeroes := make([]byte, len(pattern))
	fa, err = os.OpenFile(mediaA, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fa.WriteAt(zeroes, int64(payload.Parts[0].Start)); err != nil {
		t.Fatal(err)
	}
	_ = fa.Close()
	if err := run([]string{"mbr", "part", "import", mediaA, "1", exportPath}, &out); err != nil {
		t.Fatal(err)
	}
	restored := make([]byte, len(pattern))
	fa, err = os.Open(mediaA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fa.ReadAt(restored, int64(payload.Parts[0].Start)); err != nil {
		t.Fatal(err)
	}
	_ = fa.Close()
	if !bytes.Equal(restored, pattern) {
		t.Fatal("import did not restore partition bytes")
	}

	if err := run([]string{"mbr", "part", "clone", mediaA, "1", mediaB, "1"}, &out); err != nil {
		t.Fatal(err)
	}
	cloned := make([]byte, len(pattern))
	fb, err := os.Open(mediaB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fb.ReadAt(cloned, int64(payload.Parts[0].Start)); err != nil {
		t.Fatal(err)
	}
	_ = fb.Close()
	if !bytes.Equal(cloned, pattern) {
		t.Fatal("cloned bytes do not match source partition bytes")
	}
}

func TestMbrChsGeometryMatchesLegacyProgression(t *testing.T) {
	tests := []struct {
		name        string
		sizeBytes   int64
		wantHeads   uint32
		wantSectors uint32
	}{
		{name: "32mb", sizeBytes: 32 * 1024 * 1024, wantHeads: 4, wantSectors: 17},
		{name: "64mb", sizeBytes: 64 * 1024 * 1024, wantHeads: 8, wantSectors: 17},
		{name: "128mb", sizeBytes: 128 * 1024 * 1024, wantHeads: 16, wantSectors: 17},
		{name: "256mb", sizeBytes: 256 * 1024 * 1024, wantHeads: 16, wantSectors: 63},
		{name: "512mb", sizeBytes: 512 * 1024 * 1024, wantHeads: 32, wantSectors: 63},
		{name: "1gb", sizeBytes: 1024 * 1024 * 1024, wantHeads: 64, wantSectors: 63},
		{name: "2gb", sizeBytes: 2 * 1024 * 1024 * 1024, wantHeads: 128, wantSectors: 63},
		{name: "4gb", sizeBytes: 4 * 1024 * 1024 * 1024, wantHeads: 255, wantSectors: 63},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			totalSectors := uint32(tc.sizeBytes / mbrSectorSize)
			heads, sectors := mbrChsGeometry(totalSectors)
			if heads != tc.wantHeads || sectors != tc.wantSectors {
				t.Fatalf("unexpected geometry for %s: got %d/%d, want %d/%d", tc.name, heads, sectors, tc.wantHeads, tc.wantSectors)
			}
		})
	}
}

func TestMbrMultiPartitionChsMatchesLegacyLayout(t *testing.T) {
	tmp := t.TempDir()
	media := filepath.Join(tmp, "multi-mbr.img")
	var out bytes.Buffer

	if err := run([]string{"blank", media, "64MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"mbr", "initialize", media}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"mbr", "part", "add", media, "fat32", "4MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"mbr", "part", "add", media, "fat32", "8MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"mbr", "part", "add", media, "fat32", "4MB"}, &out); err != nil {
		t.Fatal(err)
	}

	sector0, err := os.ReadFile(media)
	if err != nil {
		t.Fatal(err)
	}
	entry2 := sector0[446+16 : 446+32]
	entry3 := sector0[446+32 : 446+48]
	if got := fmt.Sprintf("%02x%02x%02x", entry2[1], entry2[2], entry2[3]); got != "030308" {
		t.Fatalf("unexpected entry2 start CHS: %s", got)
	}
	if got := fmt.Sprintf("%02x%02x%02x", entry2[5], entry2[6], entry2[7]); got != "070618" {
		t.Fatalf("unexpected entry2 end CHS: %s", got)
	}
	if got := fmt.Sprintf("%02x%02x%02x", entry3[1], entry3[2], entry3[3]); got != "070718" {
		t.Fatalf("unexpected entry3 start CHS: %s", got)
	}
	if got := fmt.Sprintf("%02x%02x%02x", entry3[5], entry3[6], entry3[7]); got != "090820" {
		t.Fatalf("unexpected entry3 end CHS: %s", got)
	}
}

func TestGptLowLevelBinaryParity(t *testing.T) {
	tmp := t.TempDir()
	media := filepath.Join(tmp, "gpt.img")
	var out bytes.Buffer

	if err := run([]string{"blank", media, "8MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"gpt", "init", media}, &out); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(media)
	if err != nil {
		t.Fatal(err)
	}
	if string(data[512:520]) != "EFI PART" {
		t.Fatalf("missing gpt header signature: %q", string(data[512:520]))
	}
	// Protective MBR partition type
	if data[446+4] != 0xee {
		t.Fatalf("missing protective mbr type: %x", data[446+4])
	}

	if err := run([]string{"gpt", "part", "add", media, "linux", "32KB"}, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"--format", "json", "gpt", "info", media}, &out); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Parts []Part `json:"parts"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Parts) != 1 {
		t.Fatalf("expected 1 gpt partition, got %d", len(payload.Parts))
	}
	if payload.Parts[0].Size != 32*1024 {
		t.Fatalf("unexpected gpt partition size: %d", payload.Parts[0].Size)
	}
	if payload.Parts[0].Start < 34*512 {
		t.Fatalf("unexpected gpt partition start: %d", payload.Parts[0].Start)
	}

	if err := run([]string{"gpt", "part", "format", media, "1", "ROOTFS"}, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"--format", "json", "gpt", "info", media}, &out); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Parts[0].Name != "ROOTFS" {
		t.Fatalf("unexpected gpt partition label: %q", payload.Parts[0].Name)
	}

	if err := run([]string{"gpt", "part", "del", media, "1"}, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"--format", "json", "gpt", "info", media}, &out); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Parts) != 0 {
		t.Fatalf("expected 0 gpt partitions after delete, got %d", len(payload.Parts))
	}
}

func TestRdbFullWorkflowParity(t *testing.T) {
	tmp := t.TempDir()
	mediaA := filepath.Join(tmp, "rdb-a.img")
	mediaB := filepath.Join(tmp, "rdb-b.img")
	fsBin := filepath.Join(tmp, "fs.bin")
	fsOut := filepath.Join(tmp, "fs-out.bin")
	partOut := filepath.Join(tmp, "part.bin")
	backup := filepath.Join(tmp, "rdb.bak")
	partIn := filepath.Join(tmp, "part-in.bin")
	var out bytes.Buffer

	if err := os.WriteFile(fsBin, []byte("filesystem-payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partIn, bytes.Repeat([]byte{0xcd}, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"blank", mediaA, "16MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"blank", mediaB, "16MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "init", mediaA}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "init", mediaB}, &out); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"rdb", "resize", mediaA, "2MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "fs", "add", mediaA, fsBin, "PDS3"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "fs", "update", mediaA, "1", "PFS3", "1.0"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "fs", "export", mediaA, "1", fsOut}, &out); err != nil {
		t.Fatal(err)
	}
	gotFs, err := os.ReadFile(fsOut)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotFs) != "filesystem-payload" {
		t.Fatalf("unexpected fs export payload: %q", string(gotFs))
	}

	if err := run([]string{"rdb", "part", "add", mediaA, "DH0", "PDS3", "4KB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "part", "add", mediaB, "DH0", "PDS3", "4KB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "part", "import", mediaA, "1", partIn}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "part", "export", mediaA, "1", partOut}, &out); err != nil {
		t.Fatal(err)
	}
	exported, err := os.ReadFile(partOut)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(exported[:4096], bytes.Repeat([]byte{0xcd}, 4096)) {
		t.Fatal("unexpected exported rdb partition payload")
	}
	if err := run([]string{"rdb", "part", "copy", mediaA, "1", mediaB, "1"}, &out); err != nil {
		t.Fatal(err)
	}
	verifyB := filepath.Join(tmp, "part-b.bin")
	if err := run([]string{"rdb", "part", "export", mediaB, "1", verifyB}, &out); err != nil {
		t.Fatal(err)
	}
	copyBytes, err := os.ReadFile(verifyB)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(copyBytes[:4096], bytes.Repeat([]byte{0xcd}, 4096)) {
		t.Fatal("unexpected copied rdb partition payload")
	}

	if err := run([]string{"rdb", "part", "move", mediaA, "1", "3145728", "--byte-offset"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "part", "kill", mediaA, "1"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "part", "format", mediaA, "1", "WORK"}, &out); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := run([]string{"--format", "json", "rdb", "info", mediaA}, &out); err != nil {
		t.Fatal(err)
	}
	var info struct {
		RdbSize    int64  `json:"rdbSize"`
		Partitions []Part `json:"partitions"`
	}
	if err := json.Unmarshal(out.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.RdbSize != 2*1024*1024 {
		t.Fatalf("unexpected rdb size: %d", info.RdbSize)
	}
	if len(info.Partitions) != 1 || info.Partitions[0].Name != "WORK" {
		t.Fatalf("unexpected rdb partition info: %+v", info.Partitions)
	}

	if err := run([]string{"rdb", "backup", mediaA, backup}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "init", mediaA}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "restore", mediaA, backup}, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"--format", "json", "rdb", "info", mediaA}, &out); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if len(info.Partitions) != 1 || info.Partitions[0].Name != "WORK" {
		t.Fatalf("unexpected restored rdb partition info: %+v", info.Partitions)
	}

	if err := run([]string{"rdb", "part", "delete", mediaA, "1"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "fs", "del", mediaA, "1"}, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"--format", "json", "rdb", "info", mediaA}, &out); err != nil {
		t.Fatal(err)
	}
	var fullInfo map[string]any
	if err := json.Unmarshal(out.Bytes(), &fullInfo); err != nil {
		t.Fatal(err)
	}
	if fs, ok := fullInfo["filesystems"].([]any); !ok || len(fs) != 0 {
		t.Fatalf("expected zero filesystems after delete: %+v", fullInfo["filesystems"])
	}
}

func TestInfoDetectsPartitionTables(t *testing.T) {
	tmp := t.TempDir()
	mbrMedia := filepath.Join(tmp, "mbr.img")
	gptMedia := filepath.Join(tmp, "gpt.img")
	rdbMedia := filepath.Join(tmp, "rdb.img")
	var out bytes.Buffer

	if err := run([]string{"blank", mbrMedia, "4MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"mbr", "init", mbrMedia}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"mbr", "part", "add", mbrMedia, "fat32", "32KB"}, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"--format", "json", "info", mbrMedia}, &out); err != nil {
		t.Fatal(err)
	}
	var mbrInfo map[string]any
	if err := json.Unmarshal(out.Bytes(), &mbrInfo); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\"MBR\"") {
		t.Fatalf("expected MBR in info output: %s", out.String())
	}

	if err := run([]string{"blank", gptMedia, "8MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"gpt", "init", gptMedia}, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"--format", "json", "info", gptMedia}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\"GPT\"") {
		t.Fatalf("expected GPT in info output: %s", out.String())
	}

	if err := run([]string{"blank", rdbMedia, "8MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "init", rdbMedia}, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"--format", "json", "info", rdbMedia}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\"RDB\"") {
		t.Fatalf("expected RDB in info output: %s", out.String())
	}
}

func TestCompressedTransferAndCompare(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.img")
	gz := filepath.Join(tmp, "out.img.gz")
	zipPath := filepath.Join(tmp, "out.img.zip")
	plainFromGz := filepath.Join(tmp, "from-gz.img")
	plainFromZip := filepath.Join(tmp, "from-zip.img")
	var out bytes.Buffer

	payload := bytes.Repeat([]byte("ABCD"), 1024)
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"transfer", src, gz}, &out); err != nil {
		t.Fatalf("transfer to gz failed: %v", err)
	}
	if err := run([]string{"transfer", src, zipPath}, &out); err != nil {
		t.Fatalf("transfer to zip failed: %v", err)
	}
	if err := run([]string{"transfer", gz, plainFromGz}, &out); err != nil {
		t.Fatalf("transfer from gz failed: %v", err)
	}
	if err := run([]string{"transfer", zipPath, plainFromZip}, &out); err != nil {
		t.Fatalf("transfer from zip failed: %v", err)
	}
	if err := run([]string{"compare", src, plainFromGz}, &out); err != nil {
		t.Fatalf("compare src/plainFromGz failed: %v", err)
	}
	if err := run([]string{"compare", src, plainFromZip}, &out); err != nil {
		t.Fatalf("compare src/plainFromZip failed: %v", err)
	}
	if err := run([]string{"compare", src, gz}, &out); err != nil {
		t.Fatalf("compare src/gz failed: %v", err)
	}
	if err := run([]string{"compare", src, zipPath}, &out); err != nil {
		t.Fatalf("compare src/zip failed: %v", err)
	}
}

func TestPartitionPathTransferAndWrite(t *testing.T) {
	tmp := t.TempDir()
	media := filepath.Join(tmp, "disk.img")
	outFile := filepath.Join(tmp, "part-out.bin")
	inFile := filepath.Join(tmp, "part-in.bin")
	var out bytes.Buffer

	if err := run([]string{"blank", media, "8MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"mbr", "init", media}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"mbr", "part", "add", media, "fat32", "64KB"}, &out); err != nil {
		t.Fatal(err)
	}

	regionPath := media + `\mbr\1`
	sourcePayload := bytes.Repeat([]byte{0x5a}, 64*1024)
	if err := os.WriteFile(inFile, sourcePayload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"write", inFile, regionPath}, &out); err != nil {
		t.Fatalf("write to partition path failed: %v", err)
	}
	if err := run([]string{"read", regionPath, outFile}, &out); err != nil {
		t.Fatalf("read from partition path failed: %v", err)
	}
	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, sourcePayload) {
		t.Fatal("partition-path read/write payload mismatch")
	}
}

func TestFsDirPartitionContainers(t *testing.T) {
	tmp := t.TempDir()
	media := filepath.Join(tmp, "disk.img")
	var out bytes.Buffer

	if err := run([]string{"blank", media, "8MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"mbr", "init", media}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"mbr", "part", "add", media, "fat32", "32KB"}, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"fs", "dir", media + `\mbr`}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1") {
		t.Fatalf("expected mbr partition listing, got: %q", out.String())
	}

	if err := run([]string{"gpt", "init", media}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"gpt", "part", "add", media, "linux", "32KB"}, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"fs", "dir", media + `\gpt`}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1") {
		t.Fatalf("expected gpt partition listing, got: %q", out.String())
	}

	if err := run([]string{"rdb", "init", media}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "part", "add", media, "DH0", "PDS3", "32KB"}, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"fs", "dir", media + `\rdb`}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1") {
		t.Fatalf("expected rdb partition listing, got: %q", out.String())
	}
}

func TestArchiveListNonZipLhaWhenSupported(t *testing.T) {
	lhaPath := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Lha", "amiga.lha")
	var out bytes.Buffer
	err := run([]string{"archive", "list", lhaPath}, &out)
	if err != nil {
		t.Skipf("lha listing not supported in this runtime: %v", err)
	}
	if strings.TrimSpace(out.String()) == "" {
		t.Fatalf("expected non-empty archive list output for lha")
	}
}

func TestNativeRdbImageInfoAndPartitionRead(t *testing.T) {
	src := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "rigid-disk-block.img")
	tmp := t.TempDir()
	media := filepath.Join(tmp, "native-rdb.img")
	outFile := filepath.Join(tmp, "part.bin")
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(media, b, 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"--format", "json", "info", media}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\"RDB\"") {
		t.Fatalf("expected RDB partition table detection: %s", out.String())
	}

	out.Reset()
	if err := run([]string{"fs", "dir", media + `\rdb`}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1") {
		t.Fatalf("expected rdb partition container entries: %s", out.String())
	}

	out.Reset()
	err = run([]string{"read", media + `\rdb\1`, outFile, "--size", "1KB"}, &out)
	if err == nil {
		t.Fatal("expected read from truncated native rdb test image to fail")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "eof") {
		t.Fatalf("unexpected read error for truncated native rdb test image: %v", err)
	}
}

func TestNativeRdbMutationCommands(t *testing.T) {
	src := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "rigid-disk-block.img")
	tmp := t.TempDir()
	media := filepath.Join(tmp, "native-rdb-mutate.img")
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(media, b, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer

	if err := run([]string{"rdb", "part", "format", media, "1", "SYS"}, &out); err != nil {
		t.Fatalf("rdb part format failed: %v", err)
	}
	if err := run([]string{"rdb", "part", "update", media, "1", "--no-mount", "true"}, &out); err != nil {
		t.Fatalf("rdb part update failed: %v", err)
	}
	if err := run([]string{"rdb", "part", "kill", media, "1", "00000000"}, &out); err != nil {
		t.Fatalf("rdb part kill failed: %v", err)
	}
	if err := run([]string{"rdb", "part", "move", media, "1", "2"}, &out); err != nil {
		t.Fatalf("rdb part move failed: %v", err)
	}
	if err := run([]string{"rdb", "fs", "update", media, "1", "--dos-type", "PDS2"}, &out); err != nil {
		t.Fatalf("rdb fs update failed: %v", err)
	}

	out.Reset()
	if err := run([]string{"--format", "json", "rdb", "info", media}, &out); err != nil {
		t.Fatalf("rdb info failed: %v", err)
	}
	var info struct {
		Partitions  []Part          `json:"partitions"`
		FileSystems []RdbFileSystem `json:"filesystems"`
	}
	if err := json.Unmarshal(out.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if len(info.Partitions) == 0 {
		t.Fatal("expected native rdb partitions")
	}
	if info.Partitions[0].Name != "SYS" {
		t.Fatalf("expected partition name SYS, got %q", info.Partitions[0].Name)
	}
	if info.Partitions[0].Status != "inactive" {
		t.Fatalf("expected partition inactive, got %q", info.Partitions[0].Status)
	}
	if info.Partitions[0].Start != 1032192 {
		t.Fatalf("expected moved start 1032192, got %d", info.Partitions[0].Start)
	}
	if len(info.FileSystems) > 0 && info.FileSystems[0].DosType != "PDS2" {
		t.Fatalf("expected fs dos type PDS2, got %q", info.FileSystems[0].DosType)
	}
}

func TestRdbFsUpdateLegacyOptionsPropagateDosType(t *testing.T) {
	tmp := t.TempDir()
	media := filepath.Join(tmp, "rdb-update.img")
	pfs3aio := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Pfs3", "pfs3aio")
	var out bytes.Buffer

	if err := run([]string{"blank", media, "64MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "init", media}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "fs", "add", media, pfs3aio, "PDS3"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "part", "add", media, "DH0", "PDS3", "8MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "filesystem", "update", media, "1", "--dos-type", "PDS2", "--name", "PFS3AIO"}, &out); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := run([]string{"--format", "json", "rdb", "info", media}, &out); err != nil {
		t.Fatal(err)
	}
	var info struct {
		Partitions  []Part          `json:"partitions"`
		FileSystems []RdbFileSystem `json:"filesystems"`
	}
	if err := json.Unmarshal(out.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if len(info.FileSystems) == 0 {
		t.Fatal("expected file systems in rdb info")
	}
	if info.FileSystems[0].DosType != "PDS2" {
		t.Fatalf("expected fs dos type PDS2, got %q", info.FileSystems[0].DosType)
	}
	if info.FileSystems[0].Path != "PFS3AIO" {
		t.Fatalf("expected fs name PFS3AIO, got %q", info.FileSystems[0].Path)
	}
	if len(info.Partitions) == 0 {
		t.Fatal("expected partitions in rdb info")
	}
	if info.Partitions[0].Type != "PDS2" {
		t.Fatalf("expected partition dos type PDS2, got %q", info.Partitions[0].Type)
	}
}

func TestRdbFsDeleteFailsWhenPartitionUsesFileSystem(t *testing.T) {
	tmp := t.TempDir()
	media := filepath.Join(tmp, "rdb-fs-delete.img")
	pfs3aio := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Pfs3", "pfs3aio")
	var out bytes.Buffer

	if err := run([]string{"blank", media, "64MB"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "init", media}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "fs", "add", media, pfs3aio, "PDS3"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"rdb", "part", "add", media, "DH0", "PDS3", "8MB"}, &out); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"rdb", "filesystem", "delete", media, "1"}, &out)
	if err == nil {
		t.Fatal("expected delete filesystem to fail when partition uses it")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "uses file system number") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNativeRdbFsUpdatePathMutatesImage(t *testing.T) {
	src := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "rigid-disk-block.img")
	tmp := t.TempDir()
	media := filepath.Join(tmp, "native-rdb-fs-update.img")
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(media, b, 0o644); err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(b)

	fsPayload := filepath.Join(tmp, "newfs.bin")
	if err := os.WriteFile(fsPayload, []byte("ABCDEF1234567890"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"rdb", "filesystem", "update", media, "1", "--path", fsPayload}, &out); err != nil {
		t.Fatalf("rdb filesystem update --path failed: %v", err)
	}
	afterBytes, err := os.ReadFile(media)
	if err != nil {
		t.Fatal(err)
	}
	after := sha256.Sum256(afterBytes)
	if before == after {
		t.Fatal("expected native rdb image to change after filesystem update --path")
	}
}

func TestRdbPartCopyZeroSizeSourceCopiesUntilEof(t *testing.T) {
	tmp := t.TempDir()
	sourceMedia := filepath.Join(tmp, "src.img")
	destinationMedia := filepath.Join(tmp, "dst.img")

	if err := createBlankFile(sourceMedia, 3*1024*1024); err != nil {
		t.Fatal(err)
	}
	if err := createBlankFile(destinationMedia, 3*1024*1024); err != nil {
		t.Fatal(err)
	}

	sourcePattern := bytes.Repeat([]byte{0xab}, 1024*1024)
	srcFile, err := os.OpenFile(sourceMedia, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srcFile.WriteAt(sourcePattern, rdbMetaStart); err != nil {
		_ = srcFile.Close()
		t.Fatal(err)
	}
	if err := srcFile.Close(); err != nil {
		t.Fatal(err)
	}

	sourceState := rdbState{
		RdbSize:   rdbMetaStart,
		FsDataEnd: rdbMetaStart,
		Fs: []rdbFileSystem{{
			Index:   1,
			Name:    "PFS3",
			DosType: "PDS3",
		}},
		Parts: []rdbPart{{
			Index:  1,
			Name:   "DH0",
			Type:   "PDS3",
			Start:  rdbMetaStart,
			Size:   0,
			Status: "active",
		}},
	}
	if err := writeRdbState(sourceMedia, sourceState); err != nil {
		t.Fatal(err)
	}
	destinationState := rdbState{
		RdbSize:   rdbMetaStart,
		FsDataEnd: rdbMetaStart,
		Fs: []rdbFileSystem{{
			Index:   1,
			Name:    "PFS3",
			DosType: "PDS3",
		}},
		Parts: []rdbPart{},
	}
	if err := writeRdbState(destinationMedia, destinationState); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"rdb", "part", "copy", sourceMedia, "1", destinationMedia}, &out); err != nil {
		t.Fatalf("rdb part copy failed: %v", err)
	}

	finalDestinationState, err := readRdbState(destinationMedia)
	if err != nil {
		t.Fatal(err)
	}
	if len(finalDestinationState.Parts) != 1 {
		t.Fatalf("expected 1 destination partition, got %d", len(finalDestinationState.Parts))
	}
	if finalDestinationState.Parts[0].Size <= 0 {
		t.Fatalf("expected destination partition size to resolve to remaining cylinders, got %d", finalDestinationState.Parts[0].Size)
	}

	sourceBytes, err := os.ReadFile(sourceMedia)
	if err != nil {
		t.Fatal(err)
	}
	destinationBytes, err := os.ReadFile(destinationMedia)
	if err != nil {
		t.Fatal(err)
	}
	srcStart := int(sourceState.Parts[0].Start)
	dstStart := int(finalDestinationState.Parts[0].Start)
	copyLen := len(sourceBytes) - srcStart
	if copyLen < 0 {
		t.Fatalf("invalid copy length %d", copyLen)
	}
	if dstStart+copyLen > len(destinationBytes) {
		t.Fatalf("destination copy range [%d:%d] exceeds destination size %d", dstStart, dstStart+copyLen, len(destinationBytes))
	}
	if !bytes.Equal(sourceBytes[srcStart:], destinationBytes[dstStart:dstStart+copyLen]) {
		t.Fatal("expected zero-size copy to copy source bytes until EOF")
	}
}

func TestParseNativeVersionFromPfs3Binary(t *testing.T) {
	pfs3aio := filepath.Join("..", "Hst.Imager.Core.Tests", "TestData", "Pfs3", "pfs3aio")
	b, err := os.ReadFile(pfs3aio)
	if err != nil {
		t.Fatal(err)
	}
	major, minor := parseNativeVersion(b)
	if major != 19 || minor != 2 {
		t.Fatalf("expected 19.2 from native version parse, got %d.%d", major, minor)
	}
}

func TestFsCopyWithUaeFsDbMetadata(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows does not allow '*' in local filenames")
	}
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "AUX"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "file1*"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "dir1*"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "dir1*", "file4."), []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"fs", "copy", src, dst, "--recursive", "--uaemetadata", "uaefsdb"}, &out); err != nil {
		t.Fatalf("fs copy with uaefsdb failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "__uae___file1_")); err != nil {
		t.Fatalf("expected __uae___file1_: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "__uae___dir1_")); err != nil {
		t.Fatalf("expected __uae___dir1_: %v", err)
	}
	uaeFsDb := filepath.Join(dst, "_UAEFSDB.___")
	b, err := os.ReadFile(uaeFsDb)
	if err != nil {
		t.Fatalf("expected _UAEFSDB.___: %v", err)
	}
	if len(b) == 0 || len(b)%uaeFsDbNodeV1Size != 0 {
		t.Fatalf("unexpected uaefsdb size %d", len(b))
	}
	found := map[string]bool{}
	for off := 0; off+uaeFsDbNodeV1Size <= len(b); off += uaeFsDbNodeV1Size {
		record := readUaeFsDbRecord(b[off : off+uaeFsDbNodeV1Size])
		if record.Valid != 1 {
			t.Fatalf("expected valid record at offset %d, got %d", off, record.Valid)
		}
		found[record.AmigaName] = true
	}
	if !found["file1*"] || !found["dir1*"] {
		t.Fatalf("uaefsdb missing expected amiga names, got: %+v", found)
	}
}

func TestFsCopyWithUaeMetafileMetadata(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows does not allow '*' in local filenames")
	}
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "file1*"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"fs", "copy", src, dst, "--recursive", "--uaemetadata", "uaemetafile"}, &out); err != nil {
		t.Fatalf("fs copy with uaemetafile failed: %v", err)
	}
	encoded := "file1%2a"
	if _, err := os.Stat(filepath.Join(dst, encoded)); err != nil {
		t.Fatalf("expected encoded file %s: %v", encoded, err)
	}
	uaem := filepath.Join(dst, encoded+".uaem")
	b, err := os.ReadFile(uaem)
	if err != nil {
		t.Fatalf("expected uaem sidecar: %v", err)
	}
	uaemPattern := regexp.MustCompile(`^----rwed \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{2} \n$`)
	if !uaemPattern.Match(b) {
		t.Fatalf("unexpected uaem content: %q", string(b))
	}
}

func TestFsCopyPropagatesSourceUaeFsDbProperties(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "locale"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	pb := 32
	if err := writeUaeFsDb(src, "locale", "locale", &pb, "file comment"); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"fs", "copy", src, dst, "--recursive", "--uaemetadata", "uaefsdb"}, &out); err != nil {
		t.Fatalf("fs copy failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "locale")); err != nil {
		t.Fatalf("expected copied file locale: %v", err)
	}
	records, err := readUaeFsDbRecords(filepath.Join(dst, "_UAEFSDB.___"))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Fatal("expected uaefsdb records in destination")
	}
	var record *uaeFsDbRecord
	for i := range records {
		if records[i].AmigaName == "locale" {
			record = &records[i]
			break
		}
	}
	if record == nil {
		t.Fatalf("expected locale metadata record, got %+v", records)
	}
	if int(record.Mode) != pb {
		t.Fatalf("expected mode %d, got %d", pb, record.Mode)
	}
	if record.Comment != "file comment" {
		t.Fatalf("expected comment 'file comment', got %q", record.Comment)
	}
}

func TestFsCopyPropagatesSourceUaeMetafileProperties(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "safe.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	pb := 123
	date := time.Date(2024, 1, 2, 3, 4, 5, 0, time.Local)
	if err := writeUaeMetafile(src, "safe.txt", &pb, date, "source comment"); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"fs", "copy", src, dst, "--recursive", "--uaemetadata", "uaemetafile"}, &out); err != nil {
		t.Fatalf("fs copy failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "safe.txt")); err != nil {
		t.Fatalf("expected copied file safe.txt: %v", err)
	}
	gotPb, gotComment, err := readUaeMetafile(filepath.Join(dst, "safe.txt.uaem"))
	if err != nil {
		t.Fatal(err)
	}
	if gotPb == nil || *gotPb != pb {
		t.Fatalf("expected uaem protection bits %d, got %#v", pb, gotPb)
	}
	if gotComment != "source comment" {
		t.Fatalf("expected source comment, got %q", gotComment)
	}
}

func TestUaeFsDbNameMappingParitySamples(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "__uae___file1_"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !hasSpecialFilenameCharsUaeFsDb("dir1ß") {
		t.Fatal("expected dir1ß to require uaefsdb metadata")
	}
	if safe := makeSafeFilenameForUaeFsDb("dir1ß"); safe != "dir1_" {
		t.Fatalf("unexpected safe filename: %q", safe)
	}
	if safe := makeSafeFilenameForUaeFsDb(".. "); safe != "___" {
		t.Fatalf("unexpected safe filename for '.. ': %q", safe)
	}
	if safe := makeSafeFilenameForUaeFsDb("a  b"); safe != "a  b" {
		t.Fatalf("unexpected safe filename for 'a  b': %q", safe)
	}

	mapped, changed, _ := mapLocalNameForUae("file1*", "uaefsdb", tmp)
	if !changed {
		t.Fatal("expected mapped name for file1*")
	}
	if !regexp.MustCompile(`^__uae___file1_[_a-zA-Z0-9]{8}$`).MatchString(mapped) {
		t.Fatalf("unexpected mapped collision filename: %q", mapped)
	}
}

func TestUaeMetafileEncodingParitySamples(t *testing.T) {
	cases := []struct {
		name    string
		hasSpec bool
		encoded string
	}{
		{name: "a b", hasSpec: false, encoded: "a b"},
		{name: "a ", hasSpec: true, encoded: "a%20"},
		{name: "a  ", hasSpec: true, encoded: "a %20"},
		{name: ".", hasSpec: true, encoded: "%2e"},
		{name: "..", hasSpec: true, encoded: ".%2e"},
		{name: "...", hasSpec: true, encoded: "..%2e"},
		{name: ".dot", hasSpec: false, encoded: ".dot"},
		{name: "aßb", hasSpec: true, encoded: "a%dfb"},
	}
	for _, tc := range cases {
		if got := hasSpecialFilenameCharsUaeMetafile(tc.name); got != tc.hasSpec {
			t.Fatalf("hasSpecial(%q): expected %v, got %v", tc.name, tc.hasSpec, got)
		}
		if got := encodeFilenameSpecialCharsForUaeMetafile(tc.name); got != tc.encoded {
			t.Fatalf("encodeSpecial(%q): expected %q, got %q", tc.name, tc.encoded, got)
		}
	}
	if got := encodeFilenameForUaeMetafile("AUX.info"); got != "%41%55%58%2e%69%6e%66%6f" {
		t.Fatalf("unexpected full encoding for AUX.info: %q", got)
	}
}

func TestWriteUaeFsDbBinaryLayout(t *testing.T) {
	tmp := t.TempDir()
	if err := writeUaeFsDb(tmp, "AUX", "__uae___AUX", nil, ""); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(tmp, "_UAEFSDB.___"))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != uaeFsDbNodeV1Size {
		t.Fatalf("expected %d bytes, got %d", uaeFsDbNodeV1Size, len(b))
	}
	if b[0] != 1 {
		t.Fatalf("expected valid=1, got %d", b[0])
	}
	if mode := binary.BigEndian.Uint32(b[1:5]); mode != 0 {
		t.Fatalf("expected mode=0, got %d", mode)
	}
	record := readUaeFsDbRecord(b)
	if record.AmigaName != "AUX" || record.NormalName != "__uae___AUX" {
		t.Fatalf("unexpected uaefsdb record %+v", record)
	}

	pb := 123
	if err := writeUaeFsDb(tmp, "AUX", "__uae___AUX", &pb, "note"); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(filepath.Join(tmp, "_UAEFSDB.___"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := binary.BigEndian.Uint32(b[1:5]); mode != 123 {
		t.Fatalf("expected mode=123, got %d", mode)
	}
	record = readUaeFsDbRecord(b)
	if record.Comment != "note" {
		t.Fatalf("expected comment note, got %q", record.Comment)
	}
}

func TestWriteUaeMetafileFormat(t *testing.T) {
	tmp := t.TempDir()
	date := time.Date(2024, 1, 2, 3, 4, 5, 0, time.Local)
	if err := writeUaeMetafile(tmp, "AUX", nil, date, ""); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(tmp, "AUX.uaem"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != "----rwed 2024-01-02 03:04:05.00 \n" {
		t.Fatalf("unexpected default uaem content %q", got)
	}

	pb := 123
	if err := writeUaeMetafile(tmp, "AUX2", &pb, date, "hello"); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(filepath.Join(tmp, "AUX2.uaem"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != "-spa-w-- 2024-01-02 03:04:05.00 hello\n" {
		t.Fatalf("unexpected explicit uaem content %q", got)
	}
}

func TestFsDirReadsUaeFsDbMetadata(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "__uae___file1_"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "plain.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	pb := 116
	if err := writeUaeFsDb(tmp, "file1*", "__uae___file1_", &pb, "hello"); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"--format", "json", "fs", "dir", tmp, "--uaemetadata", "uaefsdb"}, &out); err != nil {
		t.Fatalf("fs dir with uaefsdb failed: %v", err)
	}
	var result struct {
		Entries []fsDirItem `json:"entries"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	names := map[string]fsDirItem{}
	for _, e := range result.Entries {
		names[e.Name] = e
	}
	if _, ok := names["_UAEFSDB.___"]; ok {
		t.Fatal("metadata file should be hidden in fs dir")
	}
	entry, ok := names["file1*"]
	if !ok {
		t.Fatalf("expected mapped amiga name file1*, got %#v", names)
	}
	if entry.ProtectionBits == nil || *entry.ProtectionBits != pb {
		t.Fatalf("expected protection bits %d, got %#v", pb, entry.ProtectionBits)
	}
	if entry.Comment != "hello" {
		t.Fatalf("expected comment hello, got %q", entry.Comment)
	}
	if _, ok := names["plain.txt"]; !ok {
		t.Fatalf("expected plain.txt entry, got %#v", names)
	}
}

func TestFsDirReadsUaeMetafileMetadata(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "file1%2a"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	pb := 123
	date := time.Date(2024, 1, 2, 3, 4, 5, 0, time.Local)
	if err := writeUaeMetafile(tmp, "file1%2a", &pb, date, "note"); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"--format", "json", "fs", "dir", tmp, "--uaemetadata", "uaemetafile"}, &out); err != nil {
		t.Fatalf("fs dir with uaemetafile failed: %v", err)
	}
	var result struct {
		Entries []fsDirItem `json:"entries"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	names := map[string]fsDirItem{}
	for _, e := range result.Entries {
		names[e.Name] = e
	}
	if _, ok := names["file1%2a.uaem"]; ok {
		t.Fatal("uaem sidecar should be hidden in fs dir")
	}
	entry, ok := names["file1*"]
	if !ok {
		t.Fatalf("expected decoded amiga name file1*, got %#v", names)
	}
	if entry.ProtectionBits == nil || *entry.ProtectionBits != pb {
		t.Fatalf("expected protection bits %d, got %#v", pb, entry.ProtectionBits)
	}
	if entry.Comment != "note" {
		t.Fatalf("expected note comment, got %q", entry.Comment)
	}
}

func TestFsDirRecursiveUsesUaeMetadataPathMapping(t *testing.T) {
	tmp := t.TempDir()
	rootDir := filepath.Join(tmp, "__uae___dir1_")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "__uae___file2_"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeUaeFsDb(tmp, "dir1*", "__uae___dir1_", nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := writeUaeFsDb(rootDir, "file2*", "__uae___file2_", nil, ""); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"--format", "json", "fs", "dir", tmp, "--recursive", "--uaemetadata", "uaefsdb"}, &out); err != nil {
		t.Fatalf("fs dir recursive with uaefsdb failed: %v", err)
	}
	var result struct {
		Entries []fsDirItem `json:"entries"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range result.Entries {
		if e.Name == "dir1*/file2*" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected recursive mapped path dir1*/file2*, got %#v", result.Entries)
	}
}

func TestFsDirResolvesUaeMetadataPathComponent(t *testing.T) {
	tmp := t.TempDir()
	dirPath := filepath.Join(tmp, "__uae___dir1_")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirPath, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeUaeFsDb(tmp, "dir1*", "__uae___dir1_", nil, ""); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	amigaPath := filepath.Join(tmp, "dir1*")
	if err := run([]string{"fs", "dir", amigaPath, "--uaemetadata", "uaefsdb"}, &out); err != nil {
		t.Fatalf("fs dir by amiga path failed: %v", err)
	}
	if !strings.Contains(out.String(), "file.txt") {
		t.Fatalf("expected file.txt in output, got %q", out.String())
	}
}

func TestFsCopyResolvesUaeFsDbSourcePath(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "__uae___file1_"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeUaeFsDb(src, "file1*", "__uae___file1_", nil, ""); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"fs", "copy", filepath.Join(src, "file1*"), dst, "--uaemetadata", "uaefsdb"}, &out); err != nil {
		t.Fatalf("fs copy by amiga source path failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "__uae___file1_")); err != nil {
		t.Fatalf("expected copied normalized file: %v", err)
	}
	records, err := readUaeFsDbRecords(filepath.Join(dst, "_UAEFSDB.___"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range records {
		if r.AmigaName == "file1*" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected destination metadata for file1*, got %+v", records)
	}
}

func TestFsCopyResolvesUaeMetafileSourcePath(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "file1%2a"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeUaeMetafile(src, "file1%2a", nil, time.Date(2024, 1, 2, 3, 4, 5, 0, time.Local), ""); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"fs", "copy", filepath.Join(src, "file1*"), dst, "--uaemetadata", "uaemetafile"}, &out); err != nil {
		t.Fatalf("fs copy by amiga source path failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "file1%2a")); err != nil {
		t.Fatalf("expected copied encoded file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "file1%2a.uaem")); err != nil {
		t.Fatalf("expected copied/generated uaem sidecar: %v", err)
	}
}

func TestFsCopyLegacyBridgePassthrough(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	destination := filepath.Join(tmp, "destination")
	argsFile := filepath.Join(tmp, "args.txt")
	legacyScript := writeLegacyBridgeStub(t, tmp, argsFile, "legacy-ok")

	t.Setenv("HST_IMAGER_LEGACY_MODE", "force")
	t.Setenv("HST_IMAGER_LEGACY_BIN", legacyScript)

	var out bytes.Buffer
	if err := run([]string{"fs", "copy", source, destination, "--recursive", "--uaemetadata", "uaefsdb"}, &out); err != nil {
		t.Fatalf("legacy fs copy failed: %v", err)
	}
	if !strings.Contains(out.String(), "legacy-ok") {
		t.Fatalf("expected legacy output, got: %q", out.String())
	}

	b, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	gotArgs := splitRecordedArgsLines(b)
	wantArgs := []string{"fs", "copy", source, destination, "--recursive", "--uaemetadata", "uaefsdb"}
	if strings.Join(gotArgs, "\n") != strings.Join(wantArgs, "\n") {
		t.Fatalf("unexpected passthrough args:\nwant=%q\ngot=%q", wantArgs, gotArgs)
	}
}

func TestFsDirLegacyBridgeAddsJsonFormat(t *testing.T) {
	tmp := t.TempDir()
	diskPath := filepath.Join(tmp, "disk")
	argsFile := filepath.Join(tmp, "args.txt")
	legacyScript := writeLegacyBridgeStub(t, tmp, argsFile, "legacy-dir")

	t.Setenv("HST_IMAGER_LEGACY_MODE", "force")
	t.Setenv("HST_IMAGER_LEGACY_BIN", legacyScript)

	var out bytes.Buffer
	if err := run([]string{"--format", "json", "fs", "dir", diskPath}, &out); err != nil {
		t.Fatalf("legacy fs dir failed: %v", err)
	}
	if !strings.Contains(out.String(), "legacy-dir") {
		t.Fatalf("expected legacy output, got: %q", out.String())
	}

	b, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	gotArgs := splitRecordedArgsLines(b)
	wantArgs := []string{"fs", "dir", diskPath, "--format", "json"}
	if strings.Join(gotArgs, "\n") != strings.Join(wantArgs, "\n") {
		t.Fatalf("unexpected passthrough args:\nwant=%q\ngot=%q", wantArgs, gotArgs)
	}
}

func TestLegacyBridgeForceRoutesSettingsCommand(t *testing.T) {
	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "args.txt")
	legacyScript := writeLegacyBridgeStub(t, tmp, argsFile, "settings-legacy")

	t.Setenv("HST_IMAGER_LEGACY_MODE", "force")
	t.Setenv("HST_IMAGER_LEGACY_BIN", legacyScript)

	var out bytes.Buffer
	if err := run([]string{"settings", "list"}, &out); err != nil {
		t.Fatalf("legacy settings list failed: %v", err)
	}
	if !strings.Contains(out.String(), "settings-legacy") {
		t.Fatalf("expected legacy output, got: %q", out.String())
	}

	b, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	gotArgs := splitRecordedArgsLines(b)
	wantArgs := []string{"settings", "list"}
	if strings.Join(gotArgs, "\n") != strings.Join(wantArgs, "\n") {
		t.Fatalf("unexpected passthrough args:\nwant=%q\ngot=%q", wantArgs, gotArgs)
	}
}
