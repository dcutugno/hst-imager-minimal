package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
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
	if err := run([]string{"mbr", "part", "add", media, "FAT32", "1KB"}, &out); err != nil {
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
	if err := run([]string{"optimize", image}, &out); err != nil {
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

	if err := run([]string{"rdb", "part", "move", mediaA, "1", "3145728"}, &out); err != nil {
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
	if len(info.Partitions) != 1 || info.Partitions[0].Status != "killed" || info.Partitions[0].Name != "WORK" {
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
