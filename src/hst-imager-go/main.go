package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

type GlobalOptions struct {
	Format string
}

type DriveInfo struct {
	Path      string `json:"path"`
	Model     string `json:"model,omitempty"`
	Size      uint64 `json:"size,omitempty"`
	Removable bool   `json:"removable"`
}

type Settings map[string]string

type Part struct {
	Index    int    `json:"index"`
	Type     string `json:"type,omitempty"`
	Name     string `json:"name,omitempty"`
	Start    int64  `json:"start"`
	Size     int64  `json:"size"`
	Bootable bool   `json:"bootable,omitempty"`
	Status   string `json:"status,omitempty"`
}

type RdbFileSystem struct {
	Index   int    `json:"index"`
	Path    string `json:"path"`
	DosType string `json:"dosType,omitempty"`
	Version string `json:"version,omitempty"`
}

type MediaMetadata struct {
	MediaPath string          `json:"mediaPath"`
	MbrParts  []Part          `json:"mbrParts,omitempty"`
	GptParts  []Part          `json:"gptParts,omitempty"`
	RdbSize   int64           `json:"rdbSize,omitempty"`
	RdbParts  []Part          `json:"rdbParts,omitempty"`
	RdbFs     []RdbFileSystem `json:"rdbFileSystems,omitempty"`
}

const mbrSectorSize = 512
const rdbSignature = "RDBGO100"
const rdbHeaderSize = 16
const rdbMetaStart = int64(64 * 1024)

type mbrPartition struct {
	Index       int
	Bootable    bool
	TypeCode    byte
	StartLBA    uint32
	SectorCount uint32
}

type gptHeader struct {
	CurrentLBA     uint64
	BackupLBA      uint64
	FirstUsable    uint64
	LastUsable     uint64
	DiskGUID       [16]byte
	PartEntriesLBA uint64
	NumEntries     uint32
	EntrySize      uint32
}

type gptPartitionEntry struct {
	Index      int
	TypeGUID   [16]byte
	UniqueGUID [16]byte
	FirstLBA   uint64
	LastLBA    uint64
	Attrs      uint64
	Name       string
}

type rdbState struct {
	Native    bool            `json:"-"`
	RdbSize   int64           `json:"rdbSize"`
	FsDataEnd int64           `json:"fsDataEnd"`
	Fs        []rdbFileSystem `json:"filesystems"`
	Parts     []rdbPart       `json:"partitions"`
}

type rdbFileSystem struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	DosType    string `json:"dosType,omitempty"`
	Version    string `json:"version,omitempty"`
	DataOffset int64  `json:"dataOffset"`
	DataSize   int64  `json:"dataSize"`
}

type rdbPart struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Start  int64  `json:"start"`
	Size   int64  `json:"size"`
	Status string `json:"status"`
}

type nativeRdbContext struct {
	blockSize  int64
	cylBytes   int64
	rdskBlock  []byte
	partBlocks []nativeRdbChainBlock
	fsBlocks   []nativeRdbChainBlock
}

type nativeRdbChainBlock struct {
	blockIndex int32
	block      []byte
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	root := BuildRootCommand()

	opts, commandArgs, err := parseGlobalArgs(args, stdout, root)
	if err != nil {
		return err
	}
	if commandArgs == nil {
		return nil
	}
	commandArgs = normalizeCommandTokens(commandArgs)

	cmd, remaining := ResolveCommand(root, commandArgs)
	consumedLen := len(commandArgs) - len(remaining)
	consumedPath := strings.TrimSpace(strings.Join(commandArgs[:consumedLen], " "))

	if len(commandArgs) == 0 || (len(remaining) == 0 && len(cmd.Children) > 0) {
		PrintHelp(stdout, cmd, consumedPath)
		return nil
	}

	if len(remaining) > 0 {
		if remaining[0] == "--help" || remaining[0] == "-h" {
			PrintHelp(stdout, cmd, consumedPath)
			return nil
		}
	}

	switch consumedPath {
	case "list":
		return handleList(remaining, stdout, opts)
	case "blank":
		return handleBlank(remaining, stdout, opts)
	case "convert":
		return handleTransfer(remaining, stdout, consumedPath, opts)
	case "optimize":
		return handleOptimize(remaining, stdout, opts)
	case "format":
		return handleFormat(remaining, stdout, opts)
	case "info":
		return handleInfo(remaining, stdout, opts)
	case "transfer", "read", "write":
		return handleTransfer(remaining, stdout, consumedPath, opts)
	case "block read":
		return handleBlockRead(remaining, stdout, opts)
	case "block view":
		return handleBlockView(remaining, stdout, opts)
	case "compare":
		return handleCompare(remaining, stdout, opts)
	case "settings list":
		return handleSettingsList(remaining, stdout, opts)
	case "settings update":
		return handleSettingsUpdate(remaining, stdout, opts)
	case "fs dir":
		return handleFsDir(remaining, stdout, opts)
	case "fs copy":
		return handleFsCopy(remaining, stdout, opts)
	case "fs extract":
		return handleFsExtract(remaining, stdout, opts)
	case "fs mkdir":
		return handleFsMkdir(remaining, stdout, opts)
	case "adf create":
		return handleAdfCreate(remaining, stdout, opts)
	case "archive list":
		return handleArchiveList(remaining, stdout, opts)
	case "mbr info":
		return handleMbrInfo(remaining, stdout, opts)
	case "mbr initialize":
		return handleMbrInitialize(remaining, stdout, opts)
	case "mbr part add":
		return handleMbrPartAdd(remaining, stdout, opts)
	case "mbr part delete":
		return handleMbrPartDelete(remaining, stdout, opts)
	case "mbr part format":
		return handleMbrPartFormat(remaining, stdout, opts)
	case "mbr part export":
		return handleMbrPartExport(remaining, stdout, opts)
	case "mbr part import":
		return handleMbrPartImport(remaining, stdout, opts)
	case "mbr part clone":
		return handleMbrPartClone(remaining, stdout, opts)
	case "gpt info":
		return handleGptInfo(remaining, stdout, opts)
	case "gpt initialize":
		return handleGptInitialize(remaining, stdout, opts)
	case "gpt part add":
		return handleGptPartAdd(remaining, stdout, opts)
	case "gpt part delete":
		return handleGptPartDelete(remaining, stdout, opts)
	case "gpt part format":
		return handleGptPartFormat(remaining, stdout, opts)
	case "rdb info":
		return handleRdbInfo(remaining, stdout, opts)
	case "rdb initialize":
		return handleRdbInitialize(remaining, stdout, opts)
	case "rdb resize":
		return handleRdbResize(remaining, stdout, opts)
	case "rdb filesystem add":
		return handleRdbFsAdd(remaining, stdout, opts)
	case "rdb filesystem delete":
		return handleRdbFsDelete(remaining, stdout, opts)
	case "rdb filesystem import":
		return handleRdbFsImport(remaining, stdout, opts)
	case "rdb filesystem export":
		return handleRdbFsExport(remaining, stdout, opts)
	case "rdb filesystem update":
		return handleRdbFsUpdate(remaining, stdout, opts)
	case "rdb part add":
		return handleRdbPartAdd(remaining, stdout, opts)
	case "rdb part update":
		return handleRdbPartUpdate(remaining, stdout, opts)
	case "rdb part delete":
		return handleRdbPartDelete(remaining, stdout, opts)
	case "rdb part copy":
		return handleRdbPartCopy(remaining, stdout, opts)
	case "rdb part export":
		return handleRdbPartExport(remaining, stdout, opts)
	case "rdb part import":
		return handleRdbPartImport(remaining, stdout, opts)
	case "rdb part kill":
		return handleRdbPartKill(remaining, stdout, opts)
	case "rdb part move":
		return handleRdbPartMove(remaining, stdout, opts)
	case "rdb part format":
		return handleRdbPartFormat(remaining, stdout, opts)
	case "rdb update":
		return handleRdbUpdate(remaining, stdout, opts)
	case "rdb backup":
		return handleRdbBackup(remaining, stdout, opts)
	case "rdb restore":
		return handleRdbRestore(remaining, stdout, opts)
	case "script":
		return handleScript(remaining, stdout)
	default:
		if len(remaining) > 0 {
			return fmt.Errorf("unknown command path: %s", strings.Join(commandArgs, " "))
		}
		fmt.Fprintf(stdout, "Command '%s' is not implemented yet in Go prototype.\n", strings.Join(commandArgs, " "))
		return nil
	}
}

func parseGlobalArgs(args []string, stdout io.Writer, root *Command) (GlobalOptions, []string, error) {
	opts := GlobalOptions{Format: "table"}
	commandArgs := make([]string, 0)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if len(commandArgs) > 0 || !strings.HasPrefix(arg, "-") {
			commandArgs = append(commandArgs, args[i:]...)
			break
		}

		switch arg {
		case "--help", "-h":
			printGlobalHelp(stdout, root)
			return opts, nil, nil
		case "--version":
			fmt.Fprintln(stdout, "hst-imager-go (prototype)")
			return opts, nil, nil
		case "--verbose":
			continue
		case "--log-file":
			if i+1 >= len(args) {
				return opts, nil, fmt.Errorf("missing value for global option: %s", arg)
			}
			i++
			continue
		case "--format":
			if i+1 >= len(args) {
				return opts, nil, fmt.Errorf("missing value for global option: %s", arg)
			}
			opts.Format = strings.ToLower(strings.TrimSpace(args[i+1]))
			if opts.Format != "table" && opts.Format != "json" {
				return opts, nil, fmt.Errorf("unsupported format '%s' (supported: table, json)", args[i+1])
			}
			i++
			continue
		default:
			return opts, nil, fmt.Errorf("unknown global option: %s", arg)
		}
	}

	return opts, commandArgs, nil
}

func printGlobalHelp(stdout io.Writer, root *Command) {
	fmt.Fprintln(stdout, "Hst Imager Go prototype")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Global options:")
	fmt.Fprintln(stdout, "  --verbose          Verbose output")
	fmt.Fprintln(stdout, "  --log-file <path>  Write log file")
	fmt.Fprintln(stdout, "  --format <type>    Output format (table|json)")
	fmt.Fprintln(stdout, "  --version          Show version")
	fmt.Fprintln(stdout)
	PrintHelp(stdout, root, "")
}

func handleList(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) != 0 {
		return errors.New("usage: list")
	}
	drives, err := listPhysicalDrives()
	if err != nil {
		return err
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"drives": drives})
	}
	if len(drives) == 0 {
		fmt.Fprintln(stdout, "No removable drives found.")
		return nil
	}
	for _, d := range drives {
		fmt.Fprintf(stdout, "- %s", d.Path)
		if d.Model != "" {
			fmt.Fprintf(stdout, " (%s)", d.Model)
		}
		if d.Size > 0 {
			fmt.Fprintf(stdout, " %d bytes", d.Size)
		}
		fmt.Fprintln(stdout)
	}
	return nil
}

func listPhysicalDrives() ([]DriveInfo, error) {
	switch runtime.GOOS {
	case "linux":
		return listLinuxDrives()
	case "darwin":
		return listDarwinDrives()
	case "windows":
		return listWindowsDrives()
	default:
		return nil, fmt.Errorf("list is not supported on OS: %s", runtime.GOOS)
	}
}

func listLinuxDrives() ([]DriveInfo, error) {
	out, err := exec.Command("lsblk", "-J", "-b", "-o", "NAME,PATH,SIZE,MODEL,RM,TYPE,TRAN").Output()
	if err != nil {
		return nil, err
	}
	type block struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Model string `json:"model"`
		Size  uint64 `json:"size"`
		Rm    bool   `json:"rm"`
		Type  string `json:"type"`
		Tran  string `json:"tran"`
	}
	var payload struct {
		Blockdevices []block `json:"blockdevices"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, err
	}
	drives := make([]DriveInfo, 0)
	for _, b := range payload.Blockdevices {
		if b.Type != "disk" {
			continue
		}
		removable := b.Rm || strings.EqualFold(b.Tran, "usb")
		if !removable {
			continue
		}
		path := b.Path
		if path == "" && b.Name != "" {
			path = "/dev/" + strings.TrimSpace(b.Name)
		}
		drives = append(drives, DriveInfo{Path: strings.TrimSpace(path), Model: strings.TrimSpace(b.Model), Size: b.Size, Removable: true})
	}
	return drives, nil
}

func listDarwinDrives() ([]DriveInfo, error) {
	out, err := exec.Command("diskutil", "list", "external", "physical").Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(out), "\n")
	drives := make([]DriveInfo, 0)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "/dev/disk") {
			drives = append(drives, DriveInfo{Path: strings.Fields(line)[0], Removable: true})
		}
	}
	return drives, nil
}

func listWindowsDrives() ([]DriveInfo, error) {
	script := "Get-CimInstance Win32_DiskDrive | Select-Object DeviceID,Model,Size,MediaType,InterfaceType | ConvertTo-Json -Compress"
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).Output()
	if err != nil {
		return nil, err
	}
	type disk struct {
		DeviceID      string `json:"DeviceID"`
		Model         string `json:"Model"`
		Size          string `json:"Size"`
		MediaType     string `json:"MediaType"`
		InterfaceType string `json:"InterfaceType"`
	}
	var many []disk
	if err := json.Unmarshal(out, &many); err != nil {
		var one disk
		if err2 := json.Unmarshal(out, &one); err2 != nil {
			return nil, err
		}
		many = []disk{one}
	}
	drives := make([]DriveInfo, 0)
	for _, d := range many {
		isRemovable := strings.Contains(strings.ToLower(d.MediaType), "removable") || strings.EqualFold(d.InterfaceType, "USB")
		if !isRemovable {
			continue
		}
		sz, _ := strconv.ParseUint(strings.TrimSpace(d.Size), 10, 64)
		drives = append(drives, DriveInfo{Path: d.DeviceID, Model: strings.TrimSpace(d.Model), Size: sz, Removable: true})
	}
	return drives, nil
}

func handleFsDir(args []string, stdout io.Writer, opts GlobalOptions) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}
	if archivePath, innerPath, ok := splitArchivePath(path); ok {
		return handleFsDirArchive(archivePath, innerPath, stdout, opts)
	}
	if basePath, table, ok := parsePartitionContainerPath(path); ok {
		return handleFsDirPartitionContainer(basePath, table, stdout, opts)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	type item struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	items := make([]item, 0, len(entries))
	for _, e := range entries {
		t := "file"
		if e.IsDir() {
			t = "dir"
		}
		items = append(items, item{Name: e.Name(), Type: t})
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"path": path, "entries": items})
	}
	for _, i := range items {
		fmt.Fprintf(stdout, "- %-4s %s\n", i.Type, i.Name)
	}
	return nil
}

func handleFsDirArchive(archivePath, innerPath string, stdout io.Writer, opts GlobalOptions) error {
	entries, err := listArchiveEntries(archivePath)
	if err != nil {
		return err
	}
	type item struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Size int64  `json:"size"`
	}
	items := make([]item, 0)
	prefix := normalizeArchivePath(innerPath)
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	seen := map[string]bool{}
	for _, e := range entries {
		name := normalizeArchivePath(e.Name)
		if prefix != "" {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			name = strings.TrimPrefix(name, prefix)
		}
		name = strings.TrimPrefix(name, "/")
		if name == "" {
			continue
		}
		parts := strings.SplitN(name, "/", 2)
		if len(parts) == 1 {
			key := parts[0]
			if seen[key] {
				continue
			}
			seen[key] = true
			items = append(items, item{Name: key, Type: "file", Size: e.Size})
			continue
		}
		key := parts[0]
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, item{Name: key, Type: "dir", Size: 0})
	}
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name) })
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"path": archivePath, "innerPath": innerPath, "entries": items})
	}
	if len(items) == 0 {
		fmt.Fprintln(stdout, "No entries.")
		return nil
	}
	for _, i := range items {
		if i.Type == "dir" {
			fmt.Fprintf(stdout, "- %-4s %s\n", i.Type, i.Name)
		} else {
			fmt.Fprintf(stdout, "- %-4s %s (%d bytes)\n", i.Type, i.Name, i.Size)
		}
	}
	return nil
}

func handleFsDirPartitionContainer(basePath, table string, stdout io.Writer, opts GlobalOptions) error {
	type entry struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Size int64  `json:"size"`
	}
	items := make([]entry, 0)
	switch table {
	case "mbr":
		parts, err := readMbrPartitions(basePath)
		if err != nil {
			return err
		}
		for _, p := range parts {
			items = append(items, entry{
				Name: strconv.Itoa(p.Index),
				Type: "part",
				Size: int64(p.SectorCount) * mbrSectorSize,
			})
		}
	case "gpt":
		_, parts, err := readGpt(basePath)
		if err != nil {
			return err
		}
		for _, p := range parts {
			items = append(items, entry{
				Name: strconv.Itoa(p.Index),
				Type: "part",
				Size: int64(p.LastLBA-p.FirstLBA+1) * mbrSectorSize,
			})
		}
	case "rdb":
		state, err := readRdbState(basePath)
		if err != nil {
			return err
		}
		for _, p := range state.Parts {
			items = append(items, entry{
				Name: strconv.Itoa(p.Index),
				Type: "part",
				Size: p.Size,
			})
		}
	default:
		return fmt.Errorf("unsupported partition table container: %s", table)
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{
			"path":    basePath + `\` + table,
			"entries": items,
		})
	}
	if len(items) == 0 {
		fmt.Fprintln(stdout, "No partitions.")
		return nil
	}
	for _, i := range items {
		fmt.Fprintf(stdout, "- %-4s %s (%d bytes)\n", i.Type, i.Name, i.Size)
	}
	return nil
}

func handleArchiveList(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 1 {
		return errors.New("usage: archive list <path-to-archive>")
	}
	items, err := listArchiveEntries(args[0])
	if err != nil {
		return err
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"path": args[0], "entries": items})
	}
	for _, i := range items {
		fmt.Fprintf(stdout, "- %s (%d bytes)\n", i.Name, i.Size)
	}
	return nil
}

func handleScript(args []string, stdout io.Writer) error {
	if len(args) < 1 {
		return errors.New("usage: script <path>")
	}
	f, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	lineNo := 0
	for s.Scan() {
		lineNo++
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := run(strings.Fields(line), stdout); err != nil {
			return fmt.Errorf("script line %d failed: %w", lineNo, err)
		}
	}
	return s.Err()
}

func settingsFilePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(configDir, "hst-imager-go")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

func readSettings() (Settings, error) {
	path, err := settingsFilePath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Settings{}, nil
		}
		return nil, err
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	if s == nil {
		s = Settings{}
	}
	return s, nil
}

func writeSettings(s Settings) error {
	path, err := settingsFilePath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func handleSettingsList(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) != 0 {
		return errors.New("usage: settings list")
	}
	s, err := readSettings()
	if err != nil {
		return err
	}
	if opts.Format == "json" {
		return writeJSON(stdout, s)
	}
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(stdout, "%s=%s\n", k, s[k])
	}
	if len(keys) == 0 {
		fmt.Fprintln(stdout, "No settings.")
	}
	return nil
}

func handleSettingsUpdate(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: settings update <key> <value>")
	}
	s, err := readSettings()
	if err != nil {
		return err
	}
	key, value := args[0], args[1]
	s[key] = value
	if err := writeSettings(s); err != nil {
		return err
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"updated": key, "value": value})
	}
	fmt.Fprintf(stdout, "Updated setting %s=%s\n", key, value)
	return nil
}

func handleBlank(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: blank <path> <size>")
	}
	path := args[0]
	size, err := parseSize(args[1])
	if err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := file.Truncate(size); err != nil {
		return err
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"path": path, "size": size, "status": "created"})
	}
	fmt.Fprintf(stdout, "Created blank image '%s' (%d bytes).\n", path, size)
	return nil
}

func handleInfo(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 1 {
		return errors.New("usage: info <path>")
	}
	path := args[0]
	region, isPartitionPath, err := resolvePartitionSelection(path)
	if err != nil {
		return err
	}
	statPath := path
	if isPartitionPath {
		statPath = region.BasePath
	}
	info, err := os.Stat(statPath)
	if err != nil {
		return err
	}
	fType := fileType(info)
	reportSize := info.Size()
	if isPartitionPath {
		reportSize = region.Size
	}
	mbrParts, hasMbr := tryReadMbr(statPath)
	gptHeader, gptParts, hasGpt := tryReadGpt(statPath)
	rdbStateValue, hasRdb := tryReadRdb(statPath)

	partitionTables := make([]string, 0)
	if hasMbr {
		partitionTables = append(partitionTables, "MBR")
	}
	if hasGpt {
		partitionTables = append(partitionTables, "GPT")
	}
	if hasRdb {
		partitionTables = append(partitionTables, "RDB")
	}

	if opts.Format == "json" {
		out := map[string]any{
			"path":            path,
			"type":            fType,
			"size":            reportSize,
			"partitionTables": partitionTables,
		}
		if isPartitionPath {
			out["selection"] = map[string]any{
				"basePath": statPath,
				"table":    strings.ToUpper(region.Table),
				"index":    region.Index,
				"offset":   region.Offset,
				"size":     region.Size,
			}
		}
		if hasMbr {
			parts := make([]Part, 0, len(mbrParts))
			for _, p := range mbrParts {
				parts = append(parts, mbrPartitionToPart(p))
			}
			out["mbr"] = map[string]any{"parts": parts}
		}
		if hasGpt {
			parts := make([]Part, 0, len(gptParts))
			for _, p := range gptParts {
				parts = append(parts, Part{
					Index: p.Index,
					Type:  gptGUIDToString(p.TypeGUID),
					Name:  p.Name,
					Start: int64(p.FirstLBA) * mbrSectorSize,
					Size:  int64(p.LastLBA-p.FirstLBA+1) * mbrSectorSize,
				})
			}
			out["gpt"] = map[string]any{
				"firstUsableLBA": gptHeader.FirstUsable,
				"lastUsableLBA":  gptHeader.LastUsable,
				"parts":          parts,
			}
		}
		if hasRdb {
			parts := make([]Part, 0, len(rdbStateValue.Parts))
			for _, p := range rdbStateValue.Parts {
				parts = append(parts, Part{
					Index:  p.Index,
					Name:   p.Name,
					Type:   p.Type,
					Start:  p.Start,
					Size:   p.Size,
					Status: p.Status,
				})
			}
			out["rdb"] = map[string]any{
				"rdbSize": rdbStateValue.RdbSize,
				"parts":   parts,
			}
		}
		return writeJSON(stdout, out)
	}
	fmt.Fprintf(stdout, "Path: %s\n", path)
	fmt.Fprintf(stdout, "Type: %s\n", fType)
	fmt.Fprintf(stdout, "Size: %d\n", reportSize)
	if isPartitionPath {
		fmt.Fprintf(stdout, "Selection: %s partition %d (offset %d)\n", strings.ToUpper(region.Table), region.Index, region.Offset)
	}
	if len(partitionTables) == 0 {
		fmt.Fprintln(stdout, "PartitionTables: none")
		return nil
	}
	fmt.Fprintf(stdout, "PartitionTables: %s\n", strings.Join(partitionTables, ", "))
	if hasMbr {
		fmt.Fprintf(stdout, "MBR partitions: %d\n", len(mbrParts))
	}
	if hasGpt {
		fmt.Fprintf(stdout, "GPT partitions: %d\n", len(gptParts))
	}
	if hasRdb {
		fmt.Fprintf(stdout, "RDB partitions: %d\n", len(rdbStateValue.Parts))
	}
	return nil
}

func tryReadMbr(path string) ([]mbrPartition, bool) {
	parts, err := readMbrPartitions(path)
	return parts, err == nil
}

func tryReadGpt(path string) (gptHeader, []gptPartitionEntry, bool) {
	header, parts, err := readGpt(path)
	return header, parts, err == nil
}

func tryReadRdb(path string) (rdbState, bool) {
	state, err := readRdbState(path)
	return state, err == nil
}

func handleTransfer(args []string, stdout io.Writer, command string, opts GlobalOptions) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: %s <source> <destination> [--size <bytes>]", command)
	}
	source := args[0]
	destination := args[1]
	copySize, err := parseOptionalSize(args[2:])
	if err != nil {
		return err
	}
	written, err := copyFile(source, destination, copySize)
	if err != nil {
		return err
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"source": source, "destination": destination, "bytes": written})
	}
	fmt.Fprintf(stdout, "Transferred %d bytes from '%s' to '%s'.\n", written, source, destination)
	return nil
}

func handleCompare(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: compare <source> <destination> [--size <bytes>]")
	}
	source := args[0]
	destination := args[1]
	compareSize, err := parseOptionalSize(args[2:])
	if err != nil {
		return err
	}
	ok, checked, err := compareFiles(source, destination, compareSize)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("compare failed: files differ within first %d bytes", checked)
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"equal": true, "bytesChecked": checked})
	}
	fmt.Fprintf(stdout, "Compare successful: %d bytes are identical.\n", checked)
	return nil
}

func writeJSON(stdout io.Writer, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, string(b))
	return err
}

func parseOptionalSize(args []string) (int64, error) {
	if len(args) == 0 {
		return 0, nil
	}
	if len(args) != 2 || args[0] != "--size" {
		return 0, fmt.Errorf("unsupported arguments: %s", strings.Join(args, " "))
	}
	return parseSize(args[1])
}

func parseSize(value string) (int64, error) {
	if value == "" {
		return 0, errors.New("size cannot be empty")
	}
	upper := strings.ToUpper(strings.TrimSpace(value))
	multiplier := int64(1)
	for _, suffix := range []struct {
		name string
		mul  int64
	}{
		{"KB", 1024},
		{"MB", 1024 * 1024},
		{"GB", 1024 * 1024 * 1024},
		{"K", 1024},
		{"M", 1024 * 1024},
		{"G", 1024 * 1024 * 1024},
	} {
		if strings.HasSuffix(upper, suffix.name) {
			multiplier = suffix.mul
			upper = strings.TrimSpace(strings.TrimSuffix(upper, suffix.name))
			break
		}
	}
	base, err := strconv.ParseInt(upper, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size '%s'", value)
	}
	if base < 0 {
		return 0, fmt.Errorf("size must be >= 0")
	}
	return base * multiplier, nil
}

func copyFile(source, destination string, size int64) (int64, error) {
	src, err := openSourceReader(source)
	if err != nil {
		return 0, err
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil && filepath.Dir(destination) != "." {
		return 0, err
	}
	dst, err := openDestinationWriter(destination)
	if err != nil {
		return 0, err
	}
	defer dst.Close()

	if size > 0 {
		written, err := io.CopyN(dst, src, size)
		if errors.Is(err, io.ErrShortWrite) {
			return written, errors.New("destination partition too small")
		}
		return written, err
	}
	written, err := io.Copy(dst, src)
	if errors.Is(err, io.ErrShortWrite) {
		return written, errors.New("destination partition too small")
	}
	return written, err
}

func compareFiles(source, destination string, size int64) (bool, int64, error) {
	a, err := openSourceReader(source)
	if err != nil {
		return false, 0, err
	}
	defer a.Close()
	b, err := openSourceReader(destination)
	if err != nil {
		return false, 0, err
	}
	defer b.Close()

	const bufSize = 64 * 1024
	ab := make([]byte, bufSize)
	bb := make([]byte, bufSize)
	var compared int64
	for {
		if size > 0 && compared >= size {
			return true, compared, nil
		}
		maxRead := bufSize
		if size > 0 {
			remaining := size - compared
			if remaining < int64(maxRead) {
				maxRead = int(remaining)
			}
		}
		an, aerr := a.Read(ab[:maxRead])
		if aerr != nil && !errors.Is(aerr, io.EOF) {
			return false, compared, aerr
		}
		bn, berr := b.Read(bb[:maxRead])
		if berr != nil && !errors.Is(berr, io.EOF) {
			return false, compared, berr
		}

		if an == 0 && bn == 0 {
			break
		}
		if an != bn {
			return false, compared + int64(min(an, bn)), nil
		}
		for i := 0; i < an; i++ {
			if ab[i] != bb[i] {
				return false, compared + int64(i+1), nil
			}
		}
		compared += int64(an)
	}
	return true, compared, nil
}

func fileType(info os.FileInfo) string {
	if info.IsDir() {
		return "Directory"
	}
	return "File"
}

func normalizeCommandTokens(args []string) []string {
	out := append([]string(nil), args...)
	for i, tok := range out {
		switch strings.ToLower(tok) {
		case "init":
			out[i] = "initialize"
		case "del":
			out[i] = "delete"
		case "fs":
			if i > 0 && strings.EqualFold(out[i-1], "rdb") {
				out[i] = "filesystem"
			}
		}
	}
	return out
}

func handleOptimize(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 1 {
		return errors.New("usage: optimize <path> [--size <size>|-s <size>|--rdb]")
	}
	path := args[0]
	var explicitSize int64 = -1
	useRdb := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--size", "-s":
			if i+1 >= len(args) {
				return errors.New("missing value for --size")
			}
			size, err := parseSize(args[i+1])
			if err != nil {
				return err
			}
			explicitSize = size
			i++
		case "--rdb":
			useRdb = true
		default:
			return fmt.Errorf("unsupported argument: %s", args[i])
		}
	}
	target := explicitSize
	if useRdb {
		state, err := readRdbState(path)
		if err != nil {
			return err
		}
		if state.RdbSize <= 0 {
			return errors.New("no RDB size present for media")
		}
		target = state.RdbSize
	}
	if target < 0 {
		size, err := trimTrailingZeros(path)
		if err != nil {
			return err
		}
		target = size
	} else if err := os.Truncate(path, target); err != nil {
		return err
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"path": path, "size": target})
	}
	fmt.Fprintf(stdout, "Optimized '%s' to %d bytes.\n", path, target)
	return nil
}

func trimTrailingZeros(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := info.Size()
	if size == 0 {
		return 0, nil
	}
	buf := make([]byte, 64*1024)
	lastNonZero := int64(-1)
	var offset int64
	for offset = 0; offset < size; offset += int64(len(buf)) {
		toRead := int64(len(buf))
		if offset+toRead > size {
			toRead = size - offset
		}
		n, err := f.ReadAt(buf[:toRead], offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		for i := 0; i < n; i++ {
			if buf[i] != 0 {
				lastNonZero = offset + int64(i)
			}
		}
	}
	target := int64(0)
	if lastNonZero >= 0 {
		target = lastNonZero + 1
	}
	if err := os.Truncate(path, target); err != nil {
		return 0, err
	}
	return target, nil
}

func handleFormat(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: format <path> <mbr|gpt|rdb|adf>")
	}
	path := args[0]
	formatType := strings.ToLower(args[1])
	var err error
	switch formatType {
	case "mbr":
		if err := initializeMbr(path); err != nil {
			return err
		}
	case "gpt":
		if err := initializeGpt(path); err != nil {
			return err
		}
	case "rdb":
		if err := initializeRdb(args[0]); err != nil {
			return err
		}
	case "adf":
		size := int64(901120)
		if len(args) > 2 {
			size, err = parseSize(args[2])
			if err != nil {
				return err
			}
		}
		if err := createBlankFile(path, size); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported format type: %s", formatType)
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"path": path, "type": formatType, "status": "formatted"})
	}
	fmt.Fprintf(stdout, "Formatted '%s' with %s.\n", path, formatType)
	return nil
}

func handleBlockRead(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 4 {
		return errors.New("usage: block read <path> <offset> <size> <output>")
	}
	offset, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil || offset < 0 {
		return fmt.Errorf("invalid offset: %s", args[1])
	}
	size, err := parseSize(args[2])
	if err != nil {
		return err
	}
	if size < 0 {
		return errors.New("size must be >= 0")
	}
	src, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer src.Close()
	if err := os.MkdirAll(filepath.Dir(args[3]), 0o755); err != nil && filepath.Dir(args[3]) != "." {
		return err
	}
	dst, err := os.Create(args[3])
	if err != nil {
		return err
	}
	defer dst.Close()
	if _, err := src.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	written, err := io.CopyN(dst, src, size)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"path": args[0], "offset": offset, "size": written, "output": args[3]})
	}
	fmt.Fprintf(stdout, "Read %d bytes from %s at offset %d to %s.\n", written, args[0], offset, args[3])
	return nil
}

func handleBlockView(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 3 {
		return errors.New("usage: block view <path> <offset> <size>")
	}
	offset, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil || offset < 0 {
		return fmt.Errorf("invalid offset: %s", args[1])
	}
	size, err := parseSize(args[2])
	if err != nil {
		return err
	}
	data := make([]byte, size)
	f, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := f.ReadAt(data, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	data = data[:n]
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"path": args[0], "offset": offset, "size": n, "hex": fmt.Sprintf("%x", data)})
	}
	for i := 0; i < len(data); i += 16 {
		end := i + 16
		if end > len(data) {
			end = len(data)
		}
		fmt.Fprintf(stdout, "%08x  % x\n", offset+int64(i), data[i:end])
	}
	return nil
}

func handleFsCopy(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: fs copy <source> <destination> [--recursive] [--uaemetadata <none|uaefsdb|uaemetafile>]")
	}
	fsOpts, err := parseFsOptions(args[2:])
	if err != nil {
		return err
	}
	count, err := copyPathWithOptions(args[0], args[1], fsOpts)
	if err != nil {
		return err
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{
			"source":      args[0],
			"destination": args[1],
			"entries":     count,
			"uaemetadata": fsOpts.uaeMetadata,
		})
	}
	fmt.Fprintf(stdout, "Copied %d entries from '%s' to '%s'.\n", count, args[0], args[1])
	return nil
}

func handleFsExtract(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: fs extract <source> <destination> [--recursive] [--uaemetadata <none|uaefsdb|uaemetafile>]")
	}
	fsOpts, err := parseFsOptions(args[2:])
	if err != nil {
		return err
	}
	if archivePath, innerPath, ok := splitArchivePath(args[0]); ok {
		if !fsOpts.recursive && innerPath == "" {
			return errors.New("fs extract archive requires --recursive or specific inner path")
		}
		tmpDir, err := os.MkdirTemp("", "hst-imager-go-extract-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)
		if err := extractArchive(archivePath, innerPath, tmpDir); err != nil {
			return err
		}
		if _, err := copyPathWithOptions(tmpDir, args[1], fsOpts); err != nil {
			return err
		}
		if opts.Format == "json" {
			return writeJSON(stdout, map[string]any{
				"source":      args[0],
				"destination": args[1],
				"status":      "extracted",
				"uaemetadata": fsOpts.uaeMetadata,
			})
		}
		fmt.Fprintf(stdout, "Extracted '%s' to '%s'.\n", args[0], args[1])
		return nil
	}
	if _, err := copyPathWithOptions(args[0], args[1], fsOpts); err != nil {
		return err
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{
			"source":      args[0],
			"destination": args[1],
			"status":      "extracted",
			"uaemetadata": fsOpts.uaeMetadata,
		})
	}
	fmt.Fprintf(stdout, "Extracted '%s' to '%s'.\n", args[0], args[1])
	return nil
}

func handleFsMkdir(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 1 {
		return errors.New("usage: fs mkdir <path>")
	}
	if err := os.MkdirAll(args[0], 0o755); err != nil {
		return err
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"path": args[0], "status": "created"})
	}
	fmt.Fprintf(stdout, "Created directory '%s'.\n", args[0])
	return nil
}

func handleAdfCreate(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 1 {
		return errors.New("usage: adf create <path> [size]")
	}
	size := int64(901120)
	if len(args) > 1 {
		parsed, err := parseSize(args[1])
		if err != nil {
			return err
		}
		size = parsed
	}
	if err := createBlankFile(args[0], size); err != nil {
		return err
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"path": args[0], "size": size, "status": "created"})
	}
	fmt.Fprintf(stdout, "Created ADF image '%s' (%d bytes).\n", args[0], size)
	return nil
}

func handleMbrInfo(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 1 {
		return errors.New("usage: mbr info <media>")
	}
	parts, err := readMbrPartitions(args[0])
	if err != nil {
		return err
	}
	if opts.Format == "json" {
		outParts := make([]Part, 0, len(parts))
		for _, p := range parts {
			outParts = append(outParts, mbrPartitionToPart(p))
		}
		return writeJSON(stdout, map[string]any{"media": args[0], "parts": outParts})
	}
	if len(parts) == 0 {
		fmt.Fprintln(stdout, "No MBR partitions.")
		return nil
	}
	for _, p := range parts {
		part := mbrPartitionToPart(p)
		fmt.Fprintf(stdout, "#%d type=%s start=%d size=%d name=%s\n", part.Index, part.Type, part.Start, part.Size, part.Name)
	}
	return nil
}

func handleMbrInitialize(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 1 {
		return errors.New("usage: mbr initialize <media>")
	}
	if err := initializeMbr(args[0]); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "mbr initialized", args[0])
}

func handleMbrPartAdd(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 3 {
		return errors.New("usage: mbr part add <media> <type> <size|*>")
	}
	parts, err := readMbrPartitions(args[0])
	if err != nil {
		return err
	}
	if len(parts) >= 4 {
		return errors.New("mbr supports maximum 4 primary partitions")
	}
	sizeBytes, err := resolveMbrPartSize(args[0], args[2], parts)
	if err != nil {
		return err
	}
	typeCode, err := parseMbrPartitionType(args[1])
	if err != nil {
		return err
	}
	const firstUsableLBA uint32 = 63
	nextStart := firstUsableLBA
	for _, p := range parts {
		end := p.StartLBA + p.SectorCount
		if end > nextStart {
			nextStart = end
		}
	}
	sizeSectors := uint32(sizeBytes / mbrSectorSize)
	if sizeSectors == 0 {
		return errors.New("partition size must be at least 512 bytes")
	}
	capacitySectors, err := mediaCapacitySectors(args[0])
	if err != nil {
		return err
	}
	if uint64(nextStart)+uint64(sizeSectors) > uint64(capacitySectors) {
		return errors.New("partition does not fit in media")
	}
	p := mbrPartition{
		Index:       nextMbrPartitionIndex(parts),
		Bootable:    false,
		TypeCode:    typeCode,
		StartLBA:    nextStart,
		SectorCount: sizeSectors,
	}
	parts = append(parts, p)
	if err := writeMbrPartitions(args[0], parts); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "mbr partition added", mbrPartitionToPart(p))
}

func handleMbrPartDelete(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: mbr part delete <media> <index>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	parts, err := readMbrPartitions(args[0])
	if err != nil {
		return err
	}
	newParts := make([]mbrPartition, 0, len(parts))
	found := false
	for _, p := range parts {
		if p.Index == idx {
			found = true
			continue
		}
		newParts = append(newParts, p)
	}
	if !found {
		return fmt.Errorf("partition %d not found", idx)
	}
	if err := writeMbrPartitions(args[0], newParts); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "mbr partition deleted", idx)
}

func handleMbrPartFormat(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 3 {
		return errors.New("usage: mbr part format <media> <index> <label>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	parts, err := readMbrPartitions(args[0])
	if err != nil {
		return err
	}
	p, err := findMbrPart(parts, idx)
	if err != nil {
		return err
	}
	part := mbrPartitionToPart(p)
	part.Name = args[2]
	return printSimpleStatus(stdout, opts, "mbr partition format requested", part)
}

func handleMbrPartExport(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 3 {
		return errors.New("usage: mbr part export <media> <index> <output>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	parts, err := readMbrPartitions(args[0])
	if err != nil {
		return err
	}
	p, err := findMbrPart(parts, idx)
	if err != nil {
		return err
	}
	start := int64(p.StartLBA) * mbrSectorSize
	size := int64(p.SectorCount) * mbrSectorSize
	written, err := copyRange(args[0], args[2], start, 0, size)
	if err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "mbr partition exported", map[string]any{"index": idx, "bytes": written})
}

func handleMbrPartImport(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 3 {
		return errors.New("usage: mbr part import <media> <index> <input>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	parts, err := readMbrPartitions(args[0])
	if err != nil {
		return err
	}
	p, err := findMbrPart(parts, idx)
	if err != nil {
		return err
	}
	start := int64(p.StartLBA) * mbrSectorSize
	size := int64(p.SectorCount) * mbrSectorSize
	written, err := copyRange(args[2], args[0], 0, start, size)
	if err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "mbr partition imported", map[string]any{"index": idx, "bytes": written})
}

func handleMbrPartClone(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 4 {
		return errors.New("usage: mbr part clone <src-media> <src-index> <dst-media> <dst-index>")
	}
	srcIdx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	dstIdx, err := strconv.Atoi(args[3])
	if err != nil {
		return err
	}
	srcParts, err := readMbrPartitions(args[0])
	if err != nil {
		return err
	}
	dstParts, err := readMbrPartitions(args[2])
	if err != nil {
		return err
	}
	srcPart, err := findMbrPart(srcParts, srcIdx)
	if err != nil {
		return err
	}
	dstPart, err := findMbrPart(dstParts, dstIdx)
	if err != nil {
		return err
	}
	size := int64(srcPart.SectorCount) * mbrSectorSize
	dstSize := int64(dstPart.SectorCount) * mbrSectorSize
	if dstSize < size {
		size = dstSize
	}
	srcStart := int64(srcPart.StartLBA) * mbrSectorSize
	dstStart := int64(dstPart.StartLBA) * mbrSectorSize
	written, err := copyRange(args[0], args[2], srcStart, dstStart, size)
	if err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "mbr partition cloned", map[string]any{"bytes": written})
}

func initializeMbr(path string) error {
	sector := make([]byte, mbrSectorSize)
	sector[510] = 0x55
	sector[511] = 0xaa
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteAt(sector, 0); err != nil {
		return err
	}
	return f.Sync()
}

func readMbrPartitions(path string) ([]mbrPartition, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sector := make([]byte, mbrSectorSize)
	if _, err := io.ReadFull(f, sector); err != nil {
		return nil, err
	}
	if sector[510] != 0x55 || sector[511] != 0xaa {
		return nil, errors.New("master boot record not found")
	}
	parts := make([]mbrPartition, 0, 4)
	for i := 0; i < 4; i++ {
		offset := 446 + i*16
		boot := sector[offset]
		typeCode := sector[offset+4]
		start := binary.LittleEndian.Uint32(sector[offset+8 : offset+12])
		count := binary.LittleEndian.Uint32(sector[offset+12 : offset+16])
		if typeCode == 0 || count == 0 {
			continue
		}
		parts = append(parts, mbrPartition{
			Index:       i + 1,
			Bootable:    boot == 0x80,
			TypeCode:    typeCode,
			StartLBA:    start,
			SectorCount: count,
		})
	}
	return parts, nil
}

func writeMbrPartitions(path string, parts []mbrPartition) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	sector := make([]byte, mbrSectorSize)
	if _, err := io.ReadFull(f, sector); err != nil {
		return err
	}
	for i := 0; i < 4; i++ {
		offset := 446 + i*16
		clear(sector[offset : offset+16])
	}
	for _, p := range parts {
		if p.Index < 1 || p.Index > 4 {
			return fmt.Errorf("invalid mbr partition index: %d", p.Index)
		}
		offset := 446 + (p.Index-1)*16
		if p.Bootable {
			sector[offset] = 0x80
		}
		// CHS placeholders for modern LBA-based layout.
		sector[offset+1] = 0xfe
		sector[offset+2] = 0xff
		sector[offset+3] = 0xff
		sector[offset+4] = p.TypeCode
		sector[offset+5] = 0xfe
		sector[offset+6] = 0xff
		sector[offset+7] = 0xff
		binary.LittleEndian.PutUint32(sector[offset+8:offset+12], p.StartLBA)
		binary.LittleEndian.PutUint32(sector[offset+12:offset+16], p.SectorCount)
	}
	sector[510] = 0x55
	sector[511] = 0xaa
	if _, err := f.WriteAt(sector, 0); err != nil {
		return err
	}
	return f.Sync()
}

func mbrPartitionToPart(p mbrPartition) Part {
	return Part{
		Index:    p.Index,
		Type:     fmt.Sprintf("0x%02x", p.TypeCode),
		Start:    int64(p.StartLBA) * mbrSectorSize,
		Size:     int64(p.SectorCount) * mbrSectorSize,
		Bootable: p.Bootable,
	}
}

func nextMbrPartitionIndex(parts []mbrPartition) int {
	used := map[int]bool{}
	for _, p := range parts {
		used[p.Index] = true
	}
	for i := 1; i <= 4; i++ {
		if !used[i] {
			return i
		}
	}
	return 0
}

func findMbrPart(parts []mbrPartition, index int) (mbrPartition, error) {
	for _, p := range parts {
		if p.Index == index {
			return p, nil
		}
	}
	return mbrPartition{}, fmt.Errorf("partition %d not found", index)
}

func mediaCapacitySectors(path string) (uint32, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if info.Size() < mbrSectorSize {
		return 0, errors.New("media size too small")
	}
	return uint32(info.Size() / mbrSectorSize), nil
}

func resolveMbrPartSize(mediaPath, value string, parts []mbrPartition) (int64, error) {
	if value != "*" {
		size, err := parseSize(value)
		if err != nil {
			return 0, err
		}
		return (size / mbrSectorSize) * mbrSectorSize, nil
	}
	capacitySectors, err := mediaCapacitySectors(mediaPath)
	if err != nil {
		return 0, err
	}
	const firstUsableLBA uint32 = 63
	var usedEnd uint32 = firstUsableLBA
	for _, p := range parts {
		end := p.StartLBA + p.SectorCount
		if end > usedEnd {
			usedEnd = end
		}
	}
	if usedEnd >= capacitySectors {
		return 0, errors.New("no free sectors available")
	}
	return int64(capacitySectors-usedEnd) * mbrSectorSize, nil
}

func parseMbrPartitionType(value string) (byte, error) {
	v := strings.TrimSpace(strings.ToLower(value))
	if strings.HasPrefix(v, "0x") {
		n, err := strconv.ParseUint(v[2:], 16, 8)
		if err != nil {
			return 0, fmt.Errorf("invalid partition type: %s", value)
		}
		return byte(n), nil
	}
	switch v {
	case "fat12":
		return 0x01, nil
	case "fat16small":
		return 0x04, nil
	case "fat16":
		return 0x06, nil
	case "ntfs", "exfat":
		return 0x07, nil
	case "fat32":
		return 0x0b, nil
	case "fat16lba":
		return 0x0e, nil
	case "fat32lba":
		return 0x0c, nil
	case "pistormrdb":
		return 0x76, nil
	default:
		return 0, fmt.Errorf("unsupported partition type '%s'", value)
	}
}

func handleGptInfo(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 1 {
		return errors.New("usage: gpt info <media>")
	}
	header, parts, err := readGpt(args[0])
	if err != nil {
		return err
	}
	outParts := make([]Part, 0, len(parts))
	for _, p := range parts {
		outParts = append(outParts, Part{
			Index: p.Index,
			Type:  gptGUIDToString(p.TypeGUID),
			Name:  p.Name,
			Start: int64(p.FirstLBA) * mbrSectorSize,
			Size:  int64(p.LastLBA-p.FirstLBA+1) * mbrSectorSize,
		})
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{
			"media":          args[0],
			"parts":          outParts,
			"firstUsableLBA": header.FirstUsable,
			"lastUsableLBA":  header.LastUsable,
		})
	}
	if len(outParts) == 0 {
		fmt.Fprintln(stdout, "No GPT partitions.")
		return nil
	}
	for _, p := range outParts {
		fmt.Fprintf(stdout, "#%d type=%s start=%d size=%d name=%s\n", p.Index, p.Type, p.Start, p.Size, p.Name)
	}
	return nil
}

func handleGptInitialize(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 1 {
		return errors.New("usage: gpt initialize <media>")
	}
	if err := initializeGpt(args[0]); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "gpt initialized", args[0])
}

func handleGptPartAdd(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 3 {
		return errors.New("usage: gpt part add <media> <type> <size|*>")
	}
	header, parts, err := readGpt(args[0])
	if err != nil {
		return err
	}
	partType, err := parseGptTypeGUID(args[1])
	if err != nil {
		return err
	}
	sizeBytes, err := resolveGptPartSize(args[0], args[2], header, parts)
	if err != nil {
		return err
	}
	sizeSectors := uint64(sizeBytes / mbrSectorSize)
	if sizeSectors == 0 {
		return errors.New("partition size must be at least 512 bytes")
	}
	startLBA := header.FirstUsable
	for _, p := range parts {
		if p.LastLBA+1 > startLBA {
			startLBA = p.LastLBA + 1
		}
	}
	endLBA := startLBA + sizeSectors - 1
	if endLBA > header.LastUsable {
		return errors.New("partition does not fit in GPT usable space")
	}
	index := nextGptPartitionIndex(header.NumEntries, parts)
	if index == 0 {
		return errors.New("no available GPT partition entries")
	}
	unique, err := newGUID()
	if err != nil {
		return err
	}
	entry := gptPartitionEntry{
		Index:      index,
		TypeGUID:   partType,
		UniqueGUID: unique,
		FirstLBA:   startLBA,
		LastLBA:    endLBA,
		Name:       args[1],
	}
	parts = append(parts, entry)
	if err := writeGpt(args[0], header, parts); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "gpt partition added", Part{
		Index: entry.Index,
		Type:  gptGUIDToString(entry.TypeGUID),
		Name:  entry.Name,
		Start: int64(entry.FirstLBA) * mbrSectorSize,
		Size:  int64(entry.LastLBA-entry.FirstLBA+1) * mbrSectorSize,
	})
}

func handleGptPartDelete(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: gpt part delete <media> <index>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	header, parts, err := readGpt(args[0])
	if err != nil {
		return err
	}
	newParts := make([]gptPartitionEntry, 0, len(parts))
	found := false
	for _, p := range parts {
		if p.Index == idx {
			found = true
			continue
		}
		newParts = append(newParts, p)
	}
	if !found {
		return fmt.Errorf("partition %d not found", idx)
	}
	if err := writeGpt(args[0], header, newParts); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "gpt partition deleted", idx)
}

func handleGptPartFormat(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 3 {
		return errors.New("usage: gpt part format <media> <index> <label>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	header, parts, err := readGpt(args[0])
	if err != nil {
		return err
	}
	found := false
	for i := range parts {
		if parts[i].Index == idx {
			parts[i].Name = args[2]
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("partition %d not found", idx)
	}
	if err := writeGpt(args[0], header, parts); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "gpt partition formatted", idx)
}

func initializeGpt(path string) error {
	sectors, err := mediaCapacitySectors(path)
	if err != nil {
		return err
	}
	const entriesCount = 128
	const entrySize = 128
	entriesBytes := entriesCount * entrySize
	entriesSectors := uint32((entriesBytes + mbrSectorSize - 1) / mbrSectorSize)
	if sectors <= 2+entriesSectors+entriesSectors+1 {
		return errors.New("media too small for GPT")
	}
	backupHeaderLBA := uint64(sectors - 1)
	backupEntriesLBA := uint64(sectors - 1 - entriesSectors)
	firstUsable := uint64(2 + entriesSectors)
	lastUsable := backupEntriesLBA - 1
	diskGUID, err := newGUID()
	if err != nil {
		return err
	}
	header := gptHeader{
		CurrentLBA:     1,
		BackupLBA:      backupHeaderLBA,
		FirstUsable:    firstUsable,
		LastUsable:     lastUsable,
		DiskGUID:       diskGUID,
		PartEntriesLBA: 2,
		NumEntries:     entriesCount,
		EntrySize:      entrySize,
	}
	if err := initializeProtectiveMbr(path, sectors); err != nil {
		return err
	}
	return writeGpt(path, header, nil)
}

func initializeProtectiveMbr(path string, sectors uint32) error {
	sector := make([]byte, mbrSectorSize)
	sector[446+4] = 0xee
	binary.LittleEndian.PutUint32(sector[446+8:446+12], 1)
	binary.LittleEndian.PutUint32(sector[446+12:446+16], sectors-1)
	sector[510] = 0x55
	sector[511] = 0xaa
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteAt(sector, 0)
	return err
}

func readGpt(path string) (gptHeader, []gptPartitionEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return gptHeader{}, nil, err
	}
	defer f.Close()
	headerSector := make([]byte, mbrSectorSize)
	if _, err := f.ReadAt(headerSector, mbrSectorSize); err != nil {
		return gptHeader{}, nil, err
	}
	if string(headerSector[:8]) != "EFI PART" {
		return gptHeader{}, nil, errors.New("guid partition table not found")
	}
	headerSize := binary.LittleEndian.Uint32(headerSector[12:16])
	if headerSize < 92 || headerSize > mbrSectorSize {
		return gptHeader{}, nil, errors.New("invalid gpt header size")
	}
	h := gptHeader{
		CurrentLBA:     binary.LittleEndian.Uint64(headerSector[24:32]),
		BackupLBA:      binary.LittleEndian.Uint64(headerSector[32:40]),
		FirstUsable:    binary.LittleEndian.Uint64(headerSector[40:48]),
		LastUsable:     binary.LittleEndian.Uint64(headerSector[48:56]),
		PartEntriesLBA: binary.LittleEndian.Uint64(headerSector[72:80]),
		NumEntries:     binary.LittleEndian.Uint32(headerSector[80:84]),
		EntrySize:      binary.LittleEndian.Uint32(headerSector[84:88]),
	}
	copy(h.DiskGUID[:], headerSector[56:72])
	if h.EntrySize < 128 {
		return gptHeader{}, nil, errors.New("unsupported gpt entry size")
	}
	entriesBytes := int(h.NumEntries * h.EntrySize)
	entryBuf := make([]byte, entriesBytes)
	if _, err := f.ReadAt(entryBuf, int64(h.PartEntriesLBA)*mbrSectorSize); err != nil {
		return gptHeader{}, nil, err
	}
	parts := make([]gptPartitionEntry, 0)
	for i := 0; i < int(h.NumEntries); i++ {
		offset := i * int(h.EntrySize)
		chunk := entryBuf[offset : offset+int(h.EntrySize)]
		var typeGUID [16]byte
		copy(typeGUID[:], chunk[:16])
		if isZeroGUID(typeGUID) {
			continue
		}
		var unique [16]byte
		copy(unique[:], chunk[16:32])
		first := binary.LittleEndian.Uint64(chunk[32:40])
		last := binary.LittleEndian.Uint64(chunk[40:48])
		attrs := binary.LittleEndian.Uint64(chunk[48:56])
		name := decodeUTF16Name(chunk[56 : 56+72])
		parts = append(parts, gptPartitionEntry{
			Index:      i + 1,
			TypeGUID:   typeGUID,
			UniqueGUID: unique,
			FirstLBA:   first,
			LastLBA:    last,
			Attrs:      attrs,
			Name:       name,
		})
	}
	return h, parts, nil
}

func writeGpt(path string, header gptHeader, entries []gptPartitionEntry) error {
	entryBytes := int(header.NumEntries * header.EntrySize)
	entryBuf := make([]byte, entryBytes)
	for _, e := range entries {
		if e.Index < 1 || e.Index > int(header.NumEntries) {
			return fmt.Errorf("invalid gpt partition index: %d", e.Index)
		}
		offset := (e.Index - 1) * int(header.EntrySize)
		chunk := entryBuf[offset : offset+int(header.EntrySize)]
		copy(chunk[:16], e.TypeGUID[:])
		copy(chunk[16:32], e.UniqueGUID[:])
		binary.LittleEndian.PutUint64(chunk[32:40], e.FirstLBA)
		binary.LittleEndian.PutUint64(chunk[40:48], e.LastLBA)
		binary.LittleEndian.PutUint64(chunk[48:56], e.Attrs)
		copy(chunk[56:56+72], encodeUTF16Name(e.Name, 72))
	}
	partArrayCRC := crc32.ChecksumIEEE(entryBuf)
	entriesSectors := uint64((len(entryBuf) + mbrSectorSize - 1) / mbrSectorSize)
	backupEntriesLBA := header.BackupLBA - entriesSectors
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := writeBlocksAtLBA(f, header.PartEntriesLBA, entryBuf); err != nil {
		return err
	}
	if err := writeBlocksAtLBA(f, backupEntriesLBA, entryBuf); err != nil {
		return err
	}
	primary := buildGptHeaderSector(header, partArrayCRC)
	if _, err := f.WriteAt(primary, int64(header.CurrentLBA)*mbrSectorSize); err != nil {
		return err
	}
	backup := header
	backup.CurrentLBA = header.BackupLBA
	backup.BackupLBA = header.CurrentLBA
	backup.PartEntriesLBA = backupEntriesLBA
	backupSector := buildGptHeaderSector(backup, partArrayCRC)
	if _, err := f.WriteAt(backupSector, int64(header.BackupLBA)*mbrSectorSize); err != nil {
		return err
	}
	return f.Sync()
}

func writeBlocksAtLBA(f *os.File, lba uint64, data []byte) error {
	paddedLen := ((len(data) + mbrSectorSize - 1) / mbrSectorSize) * mbrSectorSize
	buf := make([]byte, paddedLen)
	copy(buf, data)
	_, err := f.WriteAt(buf, int64(lba)*mbrSectorSize)
	return err
}

func buildGptHeaderSector(h gptHeader, partArrayCRC uint32) []byte {
	sector := make([]byte, mbrSectorSize)
	copy(sector[:8], []byte("EFI PART"))
	binary.LittleEndian.PutUint32(sector[8:12], 0x00010000)
	binary.LittleEndian.PutUint32(sector[12:16], 92)
	binary.LittleEndian.PutUint64(sector[24:32], h.CurrentLBA)
	binary.LittleEndian.PutUint64(sector[32:40], h.BackupLBA)
	binary.LittleEndian.PutUint64(sector[40:48], h.FirstUsable)
	binary.LittleEndian.PutUint64(sector[48:56], h.LastUsable)
	copy(sector[56:72], h.DiskGUID[:])
	binary.LittleEndian.PutUint64(sector[72:80], h.PartEntriesLBA)
	binary.LittleEndian.PutUint32(sector[80:84], h.NumEntries)
	binary.LittleEndian.PutUint32(sector[84:88], h.EntrySize)
	binary.LittleEndian.PutUint32(sector[88:92], partArrayCRC)
	// Header CRC is computed with its CRC field cleared.
	binary.LittleEndian.PutUint32(sector[16:20], 0)
	headerCRC := crc32.ChecksumIEEE(sector[:92])
	binary.LittleEndian.PutUint32(sector[16:20], headerCRC)
	return sector
}

func parseGptTypeGUID(value string) ([16]byte, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "linux", "linuxdata":
		return parseGUIDString("0fc63daf-8483-4772-8e79-3d69d8477de4")
	case "efi", "esp", "efisystem":
		return parseGUIDString("c12a7328-f81f-11d2-ba4b-00a0c93ec93b")
	case "microsoft", "basicdata", "ntfs", "fat32":
		return parseGUIDString("ebd0a0a2-b9e5-4433-87c0-68b6b72699c7")
	default:
		return parseGUIDString(value)
	}
}

func resolveGptPartSize(mediaPath, value string, header gptHeader, parts []gptPartitionEntry) (int64, error) {
	if value != "*" {
		size, err := parseSize(value)
		if err != nil {
			return 0, err
		}
		return (size / mbrSectorSize) * mbrSectorSize, nil
	}
	var usedEnd uint64 = header.FirstUsable
	for _, p := range parts {
		if p.LastLBA+1 > usedEnd {
			usedEnd = p.LastLBA + 1
		}
	}
	if usedEnd > header.LastUsable {
		return 0, errors.New("no free sectors available")
	}
	return int64(header.LastUsable-usedEnd+1) * mbrSectorSize, nil
}

func nextGptPartitionIndex(numEntries uint32, parts []gptPartitionEntry) int {
	used := map[int]bool{}
	for _, p := range parts {
		used[p.Index] = true
	}
	for i := 1; i <= int(numEntries); i++ {
		if !used[i] {
			return i
		}
	}
	return 0
}

func newGUID() ([16]byte, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return b, err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return b, nil
}

func parseGUIDString(value string) ([16]byte, error) {
	var out [16]byte
	s := strings.ToLower(strings.TrimSpace(value))
	parts := strings.Split(s, "-")
	if len(parts) != 5 {
		return out, fmt.Errorf("invalid guid: %s", value)
	}
	if len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		return out, fmt.Errorf("invalid guid: %s", value)
	}
	p0, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return out, err
	}
	p1, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return out, err
	}
	p2, err := strconv.ParseUint(parts[2], 16, 16)
	if err != nil {
		return out, err
	}
	binary.LittleEndian.PutUint32(out[0:4], uint32(p0))
	binary.LittleEndian.PutUint16(out[4:6], uint16(p1))
	binary.LittleEndian.PutUint16(out[6:8], uint16(p2))
	tail := parts[3] + parts[4]
	for i := 0; i < 8; i++ {
		n, err := strconv.ParseUint(tail[i*2:i*2+2], 16, 8)
		if err != nil {
			return out, err
		}
		out[8+i] = byte(n)
	}
	return out, nil
}

func gptGUIDToString(b [16]byte) string {
	a := binary.LittleEndian.Uint32(b[0:4])
	c := binary.LittleEndian.Uint16(b[4:6])
	d := binary.LittleEndian.Uint16(b[6:8])
	return fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		a, c, d, b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15])
}

func isZeroGUID(g [16]byte) bool {
	for _, b := range g {
		if b != 0 {
			return false
		}
	}
	return true
}

func decodeUTF16Name(data []byte) string {
	u16 := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		v := binary.LittleEndian.Uint16(data[i : i+2])
		if v == 0 {
			break
		}
		u16 = append(u16, v)
	}
	return string(utf16.Decode(u16))
}

func encodeUTF16Name(value string, maxBytes int) []byte {
	encoded := utf16.Encode([]rune(value))
	maxUnits := maxBytes / 2
	if len(encoded) > maxUnits {
		encoded = encoded[:maxUnits]
	}
	out := make([]byte, maxBytes)
	for i, v := range encoded {
		binary.LittleEndian.PutUint16(out[i*2:i*2+2], v)
	}
	return out
}

func handleRdbInfo(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 1 {
		return errors.New("usage: rdb info <media>")
	}
	state, err := readRdbState(args[0])
	if err != nil {
		return err
	}
	filesystems := make([]RdbFileSystem, 0, len(state.Fs))
	for _, fs := range state.Fs {
		filesystems = append(filesystems, RdbFileSystem{
			Index:   fs.Index,
			Path:    fs.Name,
			DosType: fs.DosType,
			Version: fs.Version,
		})
	}
	partitions := make([]Part, 0, len(state.Parts))
	for _, p := range state.Parts {
		partitions = append(partitions, Part{
			Index:  p.Index,
			Name:   p.Name,
			Type:   p.Type,
			Start:  p.Start,
			Size:   p.Size,
			Status: p.Status,
		})
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{
			"media":       args[0],
			"rdbSize":     state.RdbSize,
			"partitions":  partitions,
			"filesystems": filesystems,
		})
	}
	fmt.Fprintf(stdout, "RDB size: %d\n", state.RdbSize)
	for _, fs := range filesystems {
		fmt.Fprintf(stdout, "FS #%d name=%s dosType=%s version=%s\n", fs.Index, fs.Path, fs.DosType, fs.Version)
	}
	for _, p := range partitions {
		fmt.Fprintf(stdout, "Part #%d name=%s type=%s start=%d size=%d status=%s\n", p.Index, p.Name, p.Type, p.Start, p.Size, p.Status)
	}
	return nil
}

func handleRdbInitialize(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 1 {
		return errors.New("usage: rdb initialize <media>")
	}
	if err := initializeRdb(args[0]); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb initialized", args[0])
}

func handleRdbResize(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: rdb resize <media> <size>")
	}
	size, err := parseSize(args[1])
	if err != nil {
		return err
	}
	state, err := readRdbState(args[0])
	if err != nil {
		return err
	}
	if size < rdbMetaStart {
		return fmt.Errorf("rdb size must be >= %d", rdbMetaStart)
	}
	for _, fs := range state.Fs {
		if fs.DataOffset+fs.DataSize > size {
			return errors.New("cannot shrink rdb below embedded filesystem data")
		}
	}
	for _, p := range state.Parts {
		if p.Start < size {
			return errors.New("cannot shrink rdb below partition start offset")
		}
	}
	state.RdbSize = size
	if err := writeRdbState(args[0], state); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb resized", size)
}

func handleRdbFsAdd(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: rdb filesystem add <media> <path> [dostype]")
	}
	state, err := readRdbState(args[0])
	if err != nil {
		return err
	}
	b, err := os.ReadFile(args[1])
	if err != nil {
		return err
	}
	offset := alignUp(state.FsDataEnd, mbrSectorSize)
	if offset+int64(len(b)) > state.RdbSize {
		return errors.New("rdb has insufficient space for filesystem binary, resize rdb first")
	}
	if err := writeBytesAt(args[0], offset, b); err != nil {
		return err
	}
	fs := rdbFileSystem{
		Index:      nextRdbFsIndex(state.Fs),
		Name:       filepath.Base(args[1]),
		DataOffset: offset,
		DataSize:   int64(len(b)),
	}
	if len(args) > 2 {
		fs.DosType = args[2]
	}
	state.Fs = append(state.Fs, fs)
	state.FsDataEnd = offset + int64(len(b))
	if err := writeRdbState(args[0], state); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb filesystem added", map[string]any{"index": fs.Index, "name": fs.Name, "size": fs.DataSize})
}

func handleRdbFsDelete(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: rdb filesystem delete <media> <index>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	state, err := readRdbState(args[0])
	if err != nil {
		return err
	}
	out := make([]rdbFileSystem, 0, len(state.Fs))
	found := false
	for _, fs := range state.Fs {
		if fs.Index == idx {
			found = true
			continue
		}
		out = append(out, fs)
	}
	if !found {
		return fmt.Errorf("filesystem %d not found", idx)
	}
	state.Fs = out
	if err := writeRdbState(args[0], state); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb filesystem deleted", idx)
}

func handleRdbFsImport(args []string, stdout io.Writer, opts GlobalOptions) error {
	return handleRdbFsAdd(args, stdout, opts)
}

func handleRdbFsExport(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 3 {
		return errors.New("usage: rdb filesystem export <media> <index> <output>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	state, err := readRdbState(args[0])
	if err != nil {
		return err
	}
	var target *rdbFileSystem
	for i := range state.Fs {
		if state.Fs[i].Index == idx {
			target = &state.Fs[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("filesystem %d not found", idx)
	}
	b, err := readBytesAt(args[0], target.DataOffset, target.DataSize)
	if err != nil {
		return err
	}
	if err := os.WriteFile(args[2], b, 0o644); err != nil {
		return err
	}
	written := int64(len(b))
	if err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb filesystem exported", map[string]any{"index": idx, "bytes": written})
}

func handleRdbFsUpdate(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 3 {
		return errors.New("usage: rdb filesystem update <media> <index> <dostype>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	state, err := readRdbState(args[0])
	if err != nil {
		return err
	}
	found := false
	for i := range state.Fs {
		if state.Fs[i].Index == idx {
			state.Fs[i].DosType = args[2]
			if len(args) > 3 {
				state.Fs[i].Version = args[3]
			}
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("filesystem %d not found", idx)
	}
	if err := writeRdbState(args[0], state); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb filesystem updated", idx)
}

func handleRdbPartAdd(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 4 {
		return errors.New("usage: rdb part add <media> <name> <type> <size|*>")
	}
	state, err := readRdbState(args[0])
	if err != nil {
		return err
	}
	size, err := resolveRdbPartSize(args[0], args[3], state)
	if err != nil {
		return err
	}
	start := alignUp(maxInt64(state.RdbSize, rdbUsedEnd(state.Parts)), mbrSectorSize)
	part := rdbPart{
		Index:  nextRdbPartIndex(state.Parts),
		Name:   args[1],
		Type:   args[2],
		Start:  start,
		Size:   size,
		Status: "active",
	}
	if part.Start+part.Size > mediaSize(args[0]) {
		return errors.New("partition does not fit in media")
	}
	state.Parts = append(state.Parts, part)
	if err := writeRdbState(args[0], state); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb partition added", part)
}

func handleRdbPartUpdate(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 3 {
		return errors.New("usage: rdb part update <media> <index> <name>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	state, err := readRdbState(args[0])
	if err != nil {
		return err
	}
	found := false
	for i := range state.Parts {
		if state.Parts[i].Index == idx {
			state.Parts[i].Name = args[2]
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("partition %d not found", idx)
	}
	if err := writeRdbState(args[0], state); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb partition updated", idx)
}

func handleRdbPartDelete(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: rdb part delete <media> <index>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	state, err := readRdbState(args[0])
	if err != nil {
		return err
	}
	out := make([]rdbPart, 0, len(state.Parts))
	found := false
	for _, p := range state.Parts {
		if p.Index == idx {
			found = true
			continue
		}
		out = append(out, p)
	}
	if !found {
		return fmt.Errorf("partition %d not found", idx)
	}
	state.Parts = out
	if err := writeRdbState(args[0], state); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb partition deleted", idx)
}

func handleRdbPartCopy(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 4 {
		return errors.New("usage: rdb part copy <src-media> <src-index> <dst-media> <dst-index>")
	}
	srcIdx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	dstIdx, err := strconv.Atoi(args[3])
	if err != nil {
		return err
	}
	srcState, err := readRdbState(args[0])
	if err != nil {
		return err
	}
	dstState, err := readRdbState(args[2])
	if err != nil {
		return err
	}
	srcPart, err := findRdbPart(srcState.Parts, srcIdx)
	if err != nil {
		return err
	}
	dstPart, err := findRdbPart(dstState.Parts, dstIdx)
	if err != nil {
		return err
	}
	size := srcPart.Size
	if dstPart.Size < size {
		size = dstPart.Size
	}
	written, err := copyRange(args[0], args[2], srcPart.Start, dstPart.Start, size)
	if err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb partition copied", map[string]any{"bytes": written})
}

func handleRdbPartExport(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 3 {
		return errors.New("usage: rdb part export <media> <index> <output>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	state, err := readRdbState(args[0])
	if err != nil {
		return err
	}
	p, err := findRdbPart(state.Parts, idx)
	if err != nil {
		return err
	}
	written, err := copyRange(args[0], args[2], p.Start, 0, p.Size)
	if err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb partition exported", map[string]any{"bytes": written})
}

func handleRdbPartImport(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 3 {
		return errors.New("usage: rdb part import <media> <index> <input>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	state, err := readRdbState(args[0])
	if err != nil {
		return err
	}
	p, err := findRdbPart(state.Parts, idx)
	if err != nil {
		return err
	}
	written, err := copyRange(args[2], args[0], 0, p.Start, p.Size)
	if err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb partition imported", map[string]any{"bytes": written})
}

func handleRdbPartKill(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: rdb part kill <media> <index>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	state, err := readRdbState(args[0])
	if err != nil {
		return err
	}
	found := false
	for i := range state.Parts {
		if state.Parts[i].Index == idx {
			state.Parts[i].Status = "killed"
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("partition %d not found", idx)
	}
	if err := writeRdbState(args[0], state); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb partition killed", idx)
}

func handleRdbPartMove(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 3 {
		return errors.New("usage: rdb part move <media> <index> <start>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	start, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil || start < 0 {
		return fmt.Errorf("invalid start: %s", args[2])
	}
	state, err := readRdbState(args[0])
	if err != nil {
		return err
	}
	found := false
	for i := range state.Parts {
		if state.Parts[i].Index == idx {
			state.Parts[i].Start = start
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("partition %d not found", idx)
	}
	if err := writeRdbState(args[0], state); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb partition moved", idx)
}

func handleRdbPartFormat(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 3 {
		return errors.New("usage: rdb part format <media> <index> <label>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	state, err := readRdbState(args[0])
	if err != nil {
		return err
	}
	found := false
	for i := range state.Parts {
		if state.Parts[i].Index == idx {
			state.Parts[i].Name = args[2]
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("partition %d not found", idx)
	}
	if err := writeRdbState(args[0], state); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb partition formatted", idx)
}

func handleRdbUpdate(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: rdb update <media> <size>")
	}
	return handleRdbResize(args, stdout, opts)
}

func handleRdbBackup(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: rdb backup <media> <output>")
	}
	state, err := readRdbState(args[0])
	if err != nil {
		return err
	}
	_, err = copyRange(args[0], args[1], 0, 0, state.RdbSize)
	if err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb backup written", args[1])
}

func handleRdbRestore(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: rdb restore <media> <input>")
	}
	backupInfo, err := os.Stat(args[1])
	if err != nil {
		return err
	}
	if backupInfo.Size() <= 0 {
		return errors.New("invalid backup size")
	}
	if _, err := copyRange(args[1], args[0], 0, 0, backupInfo.Size()); err != nil {
		return err
	}
	if _, err := readRdbState(args[0]); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb backup restored", args[0])
}

func initializeRdb(path string) error {
	total := mediaSize(path)
	if total <= rdbMetaStart+mbrSectorSize {
		return errors.New("media too small for rdb")
	}
	rdbSize := int64(1 * 1024 * 1024)
	if rdbSize > total/4 {
		rdbSize = alignUp(total/4, mbrSectorSize)
	}
	if rdbSize < rdbMetaStart {
		rdbSize = alignUp(rdbMetaStart, mbrSectorSize)
	}
	if rdbSize >= total {
		rdbSize = alignUp(total/2, mbrSectorSize)
	}
	state := rdbState{
		RdbSize:   rdbSize,
		FsDataEnd: rdbMetaStart,
		Fs:        []rdbFileSystem{},
		Parts:     []rdbPart{},
	}
	return writeRdbState(path, state)
}

func readRdbState(path string) (rdbState, error) {
	f, err := os.Open(path)
	if err != nil {
		return rdbState{}, err
	}
	defer f.Close()
	header := make([]byte, rdbHeaderSize)
	if _, err := io.ReadFull(f, header); err != nil {
		return rdbState{}, err
	}
	if string(header[:8]) != rdbSignature {
		if err := trySeekStart(f); err != nil {
			return rdbState{}, err
		}
		native, err := readNativeRdbState(f)
		if err == nil {
			native.Native = true
			return native, nil
		}
		return rdbState{}, errors.New("rigid disk block not found")
	}
	length := binary.LittleEndian.Uint32(header[8:12])
	wantCRC := binary.LittleEndian.Uint32(header[12:16])
	if length == 0 || length > 4*1024*1024 {
		return rdbState{}, errors.New("invalid rdb state length")
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(f, payload); err != nil {
		return rdbState{}, err
	}
	if crc32.ChecksumIEEE(payload) != wantCRC {
		return rdbState{}, errors.New("rdb state checksum mismatch")
	}
	var state rdbState
	if err := json.Unmarshal(payload, &state); err != nil {
		return rdbState{}, err
	}
	if state.FsDataEnd == 0 {
		state.FsDataEnd = rdbMetaStart
	}
	return state, nil
}

func writeRdbState(path string, state rdbState) error {
	if state.Native {
		return writeNativeRdbState(path, state)
	}
	if state.RdbSize < rdbMetaStart {
		return fmt.Errorf("invalid rdb size: %d", state.RdbSize)
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if int64(len(payload)+rdbHeaderSize) > state.RdbSize {
		return errors.New("rdb metadata exceeds rdb size")
	}
	header := make([]byte, rdbHeaderSize)
	copy(header[:8], []byte(rdbSignature))
	binary.LittleEndian.PutUint32(header[8:12], uint32(len(payload)))
	binary.LittleEndian.PutUint32(header[12:16], crc32.ChecksumIEEE(payload))
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteAt(header, 0); err != nil {
		return err
	}
	if _, err := f.WriteAt(payload, rdbHeaderSize); err != nil {
		return err
	}
	return f.Sync()
}

func trySeekStart(f *os.File) error {
	_, err := f.Seek(0, io.SeekStart)
	return err
}

func readNativeRdbState(f *os.File) (rdbState, error) {
	ctx, err := readNativeRdbContextFromFile(f)
	if err != nil {
		return rdbState{}, err
	}
	return nativeContextToState(ctx), nil
}

func writeNativeRdbState(path string, state rdbState) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	ctx, err := readNativeRdbContextFromFile(f)
	if err != nil {
		return err
	}
	if len(ctx.partBlocks) == 0 && len(state.Parts) > 0 {
		return errors.New("native rdb add requires existing PART template")
	}
	if len(ctx.fsBlocks) == 0 && len(state.Fs) > 0 {
		return errors.New("native rdb fs add requires existing FSHD template")
	}
	if err := applyNativePartState(&ctx, state); err != nil {
		return err
	}
	if err := applyNativeFsState(&ctx, state); err != nil {
		return err
	}
	return writeNativeRdbContextToFile(f, ctx)
}

func readNativeRdbContext(path string) (nativeRdbContext, error) {
	f, err := os.Open(path)
	if err != nil {
		return nativeRdbContext{}, err
	}
	defer f.Close()
	return readNativeRdbContextFromFile(f)
}

func readNativeRdbContextFromFile(f *os.File) (nativeRdbContext, error) {
	if err := trySeekStart(f); err != nil {
		return nativeRdbContext{}, err
	}
	sector := make([]byte, mbrSectorSize)
	if _, err := io.ReadFull(f, sector); err != nil {
		return nativeRdbContext{}, err
	}
	if string(sector[:4]) != "RDSK" {
		return nativeRdbContext{}, errors.New("native rdb not found")
	}
	blockSize := int64(readBeU32(sector, 0x10))
	if blockSize <= 0 {
		blockSize = mbrSectorSize
	}
	sectorsPerTrack := int64(readBeU32(sector, 0x48))
	heads := int64(readBeU32(sector, 0x4c))
	if sectorsPerTrack <= 0 {
		sectorsPerTrack = 63
	}
	if heads <= 0 {
		heads = 16
	}
	ctx := nativeRdbContext{
		blockSize: blockSize,
		cylBytes:  sectorsPerTrack * heads * blockSize,
		rdskBlock: append([]byte(nil), sector...),
	}
	partPtr := int32(readBeU32(sector, 0x1c))
	fsPtr := int32(readBeU32(sector, 0x20))
	partBlocks, err := readNativeChainBlocks(f, blockSize, partPtr, "PART")
	if err != nil {
		return nativeRdbContext{}, err
	}
	fsBlocks, err := readNativeChainBlocks(f, blockSize, fsPtr, "FSHD")
	if err != nil {
		return nativeRdbContext{}, err
	}
	ctx.partBlocks = partBlocks
	ctx.fsBlocks = fsBlocks
	return ctx, nil
}

func readNativeChainBlocks(f *os.File, blockSize int64, start int32, kind string) ([]nativeRdbChainBlock, error) {
	blocks := make([]nativeRdbChainBlock, 0)
	visited := map[int32]bool{}
	for ptr := start; ptr >= 0; {
		if visited[ptr] {
			break
		}
		visited[ptr] = true
		block, err := readBlockAt(f, int64(ptr)*blockSize, int(blockSize))
		if err != nil {
			return nil, err
		}
		if string(block[:4]) != kind {
			break
		}
		blocks = append(blocks, nativeRdbChainBlock{
			blockIndex: ptr,
			block:      block,
		})
		ptr = int32(readBeU32(block, 0x10))
	}
	return blocks, nil
}

func nativeContextToState(ctx nativeRdbContext) rdbState {
	state := rdbState{
		Native:    true,
		RdbSize:   ctx.blockSize,
		FsDataEnd: ctx.blockSize,
		Fs:        []rdbFileSystem{},
		Parts:     []rdbPart{},
	}
	maxBlock := int32(0)
	for i, pb := range ctx.partBlocks {
		if pb.blockIndex > maxBlock {
			maxBlock = pb.blockIndex
		}
		block := pb.block
		name := readBString(block, 0x24)
		flags := readBeU32(block, 0x14)
		lowCyl := int64(readBeU32(block, 0xa4))
		highCyl := int64(readBeU32(block, 0xa8))
		dosType := string(block[0xc0:0xc4])
		if highCyl < lowCyl {
			highCyl = lowCyl
		}
		status := "active"
		if flags&0x1 == 0 {
			status = "inactive"
		}
		state.Parts = append(state.Parts, rdbPart{
			Index:  i + 1,
			Name:   name,
			Type:   dosType,
			Start:  lowCyl * ctx.cylBytes,
			Size:   (highCyl - lowCyl + 1) * ctx.cylBytes,
			Status: status,
		})
	}
	for i, fb := range ctx.fsBlocks {
		if fb.blockIndex > maxBlock {
			maxBlock = fb.blockIndex
		}
		block := fb.block
		dosType := string(block[0x20:0x24])
		version := fmt.Sprintf("%d.%d", readBeU16(block, 0x24), readBeU16(block, 0x26))
		name := readAsciiCString(block, 0xac, int(ctx.blockSize-0xac))
		if name == "" {
			name = fmt.Sprintf("fs-%d", i+1)
		}
		state.Fs = append(state.Fs, rdbFileSystem{
			Index:      i + 1,
			Name:       name,
			DosType:    dosType,
			Version:    version,
			DataOffset: int64(fb.blockIndex) * ctx.blockSize,
			DataSize:   ctx.blockSize,
		})
	}
	state.RdbSize = int64(maxBlock+1) * ctx.blockSize
	if state.RdbSize < ctx.blockSize {
		state.RdbSize = ctx.blockSize
	}
	state.FsDataEnd = state.RdbSize
	return state
}

func applyNativePartState(ctx *nativeRdbContext, state rdbState) error {
	desired := len(state.Parts)
	current := len(ctx.partBlocks)
	if desired > current {
		if current == 0 {
			return errors.New("cannot add native rdb partition without template")
		}
		maxIdx := maxNativeBlockIndex(ctx)
		template := append([]byte(nil), ctx.partBlocks[current-1].block...)
		for i := current; i < desired; i++ {
			maxIdx++
			block := append([]byte(nil), template...)
			ctx.partBlocks = append(ctx.partBlocks, nativeRdbChainBlock{blockIndex: maxIdx, block: block})
		}
	}
	if desired < current {
		ctx.partBlocks = ctx.partBlocks[:desired]
	}
	for i := 0; i < len(ctx.partBlocks); i++ {
		block := ctx.partBlocks[i].block
		next := int32(-1)
		if i+1 < len(ctx.partBlocks) {
			next = ctx.partBlocks[i+1].blockIndex
		}
		writeBeU32(block, 0x10, uint32(next))
	}
	for i, p := range state.Parts {
		if i >= len(ctx.partBlocks) {
			break
		}
		block := ctx.partBlocks[i].block
		flags := readBeU32(block, 0x14)
		if p.Status == "inactive" || p.Status == "killed" {
			flags &^= 0x1
		} else {
			flags |= 0x1
		}
		writeBeU32(block, 0x14, flags)
		lowCyl := uint32(0)
		if ctx.cylBytes > 0 {
			lowCyl = uint32(maxInt64(p.Start, 0) / ctx.cylBytes)
		}
		cylCount := uint32(1)
		if ctx.cylBytes > 0 && p.Size > 0 {
			cylCount = uint32((p.Size + ctx.cylBytes - 1) / ctx.cylBytes)
			if cylCount == 0 {
				cylCount = 1
			}
		}
		highCyl := lowCyl + cylCount - 1
		writeBeU32(block, 0xa4, lowCyl)
		writeBeU32(block, 0xa8, highCyl)
		writeBString(block, 0x24, p.Name)
		writeFourCC(block, 0xc0, p.Type)
		updateAmigaBlockChecksum(block)
	}
	return nil
}

func applyNativeFsState(ctx *nativeRdbContext, state rdbState) error {
	desired := len(state.Fs)
	current := len(ctx.fsBlocks)
	if desired > current {
		if current == 0 {
			return errors.New("cannot add native rdb filesystem without template")
		}
		maxIdx := maxNativeBlockIndex(ctx)
		template := append([]byte(nil), ctx.fsBlocks[current-1].block...)
		for i := current; i < desired; i++ {
			maxIdx++
			block := append([]byte(nil), template...)
			ctx.fsBlocks = append(ctx.fsBlocks, nativeRdbChainBlock{blockIndex: maxIdx, block: block})
		}
	}
	if desired < current {
		ctx.fsBlocks = ctx.fsBlocks[:desired]
	}
	for i := 0; i < len(ctx.fsBlocks); i++ {
		block := ctx.fsBlocks[i].block
		next := int32(-1)
		if i+1 < len(ctx.fsBlocks) {
			next = ctx.fsBlocks[i+1].blockIndex
		}
		writeBeU32(block, 0x10, uint32(next))
	}
	for i, fs := range state.Fs {
		if i >= len(ctx.fsBlocks) {
			break
		}
		block := ctx.fsBlocks[i].block
		writeFourCC(block, 0x20, fs.DosType)
		if fs.Version != "" {
			parts := strings.SplitN(fs.Version, ".", 2)
			major, _ := strconv.Atoi(parts[0])
			minor := 0
			if len(parts) > 1 {
				minor, _ = strconv.Atoi(parts[1])
			}
			writeBeU16(block, 0x24, uint16(major))
			writeBeU16(block, 0x26, uint16(minor))
		}
		writeAsciiCString(block, 0xac, fs.Name)
		updateAmigaBlockChecksum(block)
	}
	return nil
}

func writeNativeRdbContextToFile(f *os.File, ctx nativeRdbContext) error {
	partPtr := int32(-1)
	if len(ctx.partBlocks) > 0 {
		partPtr = ctx.partBlocks[0].blockIndex
	}
	fsPtr := int32(-1)
	if len(ctx.fsBlocks) > 0 {
		fsPtr = ctx.fsBlocks[0].blockIndex
	}
	writeBeU32(ctx.rdskBlock, 0x1c, uint32(partPtr))
	writeBeU32(ctx.rdskBlock, 0x20, uint32(fsPtr))
	updateAmigaBlockChecksum(ctx.rdskBlock)

	if _, err := f.WriteAt(ctx.rdskBlock, 0); err != nil {
		return err
	}
	for _, pb := range ctx.partBlocks {
		updateAmigaBlockChecksum(pb.block)
		if _, err := f.WriteAt(pb.block, int64(pb.blockIndex)*ctx.blockSize); err != nil {
			return err
		}
	}
	for _, fb := range ctx.fsBlocks {
		updateAmigaBlockChecksum(fb.block)
		if _, err := f.WriteAt(fb.block, int64(fb.blockIndex)*ctx.blockSize); err != nil {
			return err
		}
	}
	return f.Sync()
}

func maxNativeBlockIndex(ctx *nativeRdbContext) int32 {
	maxIdx := int32(0)
	for _, p := range ctx.partBlocks {
		if p.blockIndex > maxIdx {
			maxIdx = p.blockIndex
		}
	}
	for _, fs := range ctx.fsBlocks {
		if fs.blockIndex > maxIdx {
			maxIdx = fs.blockIndex
		}
	}
	return maxIdx
}

func readBlockAt(f *os.File, offset int64, size int) ([]byte, error) {
	b := make([]byte, size)
	_, err := f.ReadAt(b, offset)
	return b, err
}

func readBeU32(b []byte, offset int) uint32 {
	if offset+4 > len(b) {
		return 0
	}
	return binary.BigEndian.Uint32(b[offset : offset+4])
}

func readBeU16(b []byte, offset int) uint16 {
	if offset+2 > len(b) {
		return 0
	}
	return binary.BigEndian.Uint16(b[offset : offset+2])
}

func writeBeU32(b []byte, offset int, value uint32) {
	if offset+4 > len(b) {
		return
	}
	binary.BigEndian.PutUint32(b[offset:offset+4], value)
}

func writeBeU16(b []byte, offset int, value uint16) {
	if offset+2 > len(b) {
		return
	}
	binary.BigEndian.PutUint16(b[offset:offset+2], value)
}

func readBString(b []byte, offset int) string {
	if offset >= len(b) {
		return ""
	}
	n := int(b[offset])
	if n <= 0 {
		return ""
	}
	if offset+1+n > len(b) {
		n = len(b) - offset - 1
	}
	return string(bytes.TrimSpace(b[offset+1 : offset+1+n]))
}

func readAsciiCString(b []byte, offset int, max int) string {
	if offset >= len(b) || max <= 0 {
		return ""
	}
	end := offset + max
	if end > len(b) {
		end = len(b)
	}
	buf := b[offset:end]
	n := len(buf)
	for i, c := range buf {
		if c == 0 {
			n = i
			break
		}
	}
	return strings.TrimSpace(string(buf[:n]))
}

func writeBString(b []byte, offset int, value string) {
	if offset >= len(b) {
		return
	}
	maxLen := len(b) - offset - 1
	if maxLen < 0 {
		return
	}
	if maxLen > 31 {
		maxLen = 31
	}
	v := []byte(value)
	if len(v) > maxLen {
		v = v[:maxLen]
	}
	b[offset] = byte(len(v))
	copy(b[offset+1:], v)
	end := offset + 1 + len(v)
	for i := end; i < offset+1+maxLen && i < len(b); i++ {
		b[i] = 0
	}
}

func writeFourCC(b []byte, offset int, value string) {
	if offset+4 > len(b) {
		return
	}
	raw := []byte(value)
	for i := 0; i < 4; i++ {
		if i < len(raw) {
			b[offset+i] = raw[i]
		} else {
			b[offset+i] = 0
		}
	}
}

func writeAsciiCString(b []byte, offset int, value string) {
	if offset >= len(b) {
		return
	}
	maxLen := len(b) - offset
	raw := []byte(value)
	if len(raw) >= maxLen {
		raw = raw[:maxLen-1]
	}
	copy(b[offset:], raw)
	end := offset + len(raw)
	if end < len(b) {
		b[end] = 0
	}
}

func updateAmigaBlockChecksum(block []byte) {
	if len(block) < 12 {
		return
	}
	longs := int(binary.BigEndian.Uint32(block[:4]))
	if longs <= 0 {
		return
	}
	limit := longs * 4
	if limit > len(block) {
		limit = len(block) - (len(block) % 4)
	}
	if limit < 12 {
		return
	}
	binary.BigEndian.PutUint32(block[8:12], 0)
	var sum uint32
	for i := 0; i+3 < limit; i += 4 {
		sum += binary.BigEndian.Uint32(block[i : i+4])
	}
	checksum := uint32(0) - sum
	binary.BigEndian.PutUint32(block[8:12], checksum)
}

func nextRdbFsIndex(items []rdbFileSystem) int {
	max := 0
	for _, fs := range items {
		if fs.Index > max {
			max = fs.Index
		}
	}
	return max + 1
}

func nextRdbPartIndex(items []rdbPart) int {
	max := 0
	for _, p := range items {
		if p.Index > max {
			max = p.Index
		}
	}
	return max + 1
}

func findRdbPart(items []rdbPart, index int) (rdbPart, error) {
	for _, p := range items {
		if p.Index == index {
			return p, nil
		}
	}
	return rdbPart{}, fmt.Errorf("partition %d not found", index)
}

func rdbUsedEnd(items []rdbPart) int64 {
	end := int64(0)
	for _, p := range items {
		if p.Start+p.Size > end {
			end = p.Start + p.Size
		}
	}
	return end
}

func resolveRdbPartSize(mediaPath, value string, state rdbState) (int64, error) {
	if value != "*" {
		size, err := parseSize(value)
		if err != nil {
			return 0, err
		}
		return alignUp(size, mbrSectorSize), nil
	}
	total := mediaSize(mediaPath)
	start := alignUp(maxInt64(state.RdbSize, rdbUsedEnd(state.Parts)), mbrSectorSize)
	remain := total - start
	if remain < 0 {
		remain = 0
	}
	return alignUp(remain, mbrSectorSize), nil
}

func writeBytesAt(path string, offset int64, data []byte) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteAt(data, offset)
	return err
}

func readBytesAt(path string, offset, size int64) ([]byte, error) {
	if size < 0 {
		return nil, errors.New("size must be >= 0")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b := make([]byte, size)
	_, err = io.ReadFull(io.NewSectionReader(f, offset, size), b)
	return b, err
}

func alignUp(value int64, by int64) int64 {
	if by <= 0 {
		return value
	}
	rem := value % by
	if rem == 0 {
		return value
	}
	return value + (by - rem)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func printSimpleStatus(stdout io.Writer, opts GlobalOptions, message string, details any) error {
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"status": message, "details": details})
	}
	fmt.Fprintf(stdout, "%s: %v\n", message, details)
	return nil
}

func copyPath(source, destination string, recursive bool) (int, error) {
	info, err := os.Stat(source)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		target := destination
		if dstInfo, err := os.Stat(destination); err == nil && dstInfo.IsDir() {
			target = filepath.Join(destination, filepath.Base(source))
		}
		_, err := copyFile(source, target, 0)
		return 1, err
	}
	if !recursive {
		return 0, errors.New("source is a directory, use --recursive")
	}
	count := 0
	err = filepath.Walk(source, func(path string, entry os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if _, err := copyFile(path, target, 0); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

type fsPathOptions struct {
	recursive   bool
	uaeMetadata string
}

type uaeFsDbNode struct {
	AmigaName  string `json:"amigaName"`
	NormalName string `json:"normalName"`
	Type       string `json:"type"`
}

func parseFsOptions(args []string) (fsPathOptions, error) {
	opts := fsPathOptions{
		recursive:   false,
		uaeMetadata: "uaefsdb",
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--recursive":
			opts.recursive = true
		case "--uaemetadata", "-uae":
			if i+1 >= len(args) {
				return opts, errors.New("missing value for --uaemetadata")
			}
			val := strings.ToLower(strings.TrimSpace(args[i+1]))
			switch val {
			case "none", "uaefsdb", "uaemetafile":
				opts.uaeMetadata = val
			default:
				return opts, fmt.Errorf("unsupported uaemetadata '%s' (supported: none, uaefsdb, uaemetafile)", args[i+1])
			}
			i++
		default:
			return opts, fmt.Errorf("unsupported arguments: %s", strings.Join(args[i:], " "))
		}
	}
	return opts, nil
}

func copyPathWithOptions(source, destination string, opts fsPathOptions) (int, error) {
	info, err := os.Stat(source)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		target := destination
		if dstInfo, err := os.Stat(destination); err == nil && dstInfo.IsDir() {
			mappedName, _, _ := mapLocalNameForUae(filepath.Base(source), opts.uaeMetadata)
			target = filepath.Join(destination, mappedName)
			if err := writeUaeMetadataForEntry(destination, filepath.Base(source), mappedName, "file", opts.uaeMetadata); err != nil {
				return 0, err
			}
		}
		_, err := copyFile(source, target, 0)
		return 1, err
	}
	if !opts.recursive {
		return 0, errors.New("source is a directory, use --recursive")
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return 0, err
	}
	return copyDirectoryRecursiveWithUae(source, destination, opts)
}

func copyDirectoryRecursiveWithUae(source, destination string, opts fsPathOptions) (int, error) {
	entries, err := os.ReadDir(source)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		srcPath := filepath.Join(source, entry.Name())
		mappedName, changed, _ := mapLocalNameForUae(entry.Name(), opts.uaeMetadata)
		dstPath := filepath.Join(destination, mappedName)
		if changed {
			entryType := "file"
			if entry.IsDir() {
				entryType = "dir"
			}
			if err := writeUaeMetadataForEntry(destination, entry.Name(), mappedName, entryType, opts.uaeMetadata); err != nil {
				return count, err
			}
		}
		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, 0o755); err != nil {
				return count, err
			}
			childCount, err := copyDirectoryRecursiveWithUae(srcPath, dstPath, opts)
			count += childCount
			if err != nil {
				return count, err
			}
			count++
			continue
		}
		if _, err := copyFile(srcPath, dstPath, 0); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func mapLocalNameForUae(name, mode string) (mapped string, changed bool, metaName string) {
	if mode == "none" {
		return name, false, name
	}
	if !isUnsafeWindowsName(name) {
		return name, false, name
	}
	switch mode {
	case "uaefsdb":
		return "__uae___" + sanitizeUnsafeName(name), true, name
	case "uaemetafile":
		return percentEncodeName(name), true, name
	default:
		return name, false, name
	}
}

func isUnsafeWindowsName(name string) bool {
	if name == "" {
		return false
	}
	base := name
	if dot := strings.Index(base, "."); dot >= 0 {
		base = base[:dot]
	}
	baseUpper := strings.ToUpper(strings.TrimSpace(base))
	reserved := map[string]bool{
		"CON": true, "PRN": true, "AUX": true, "NUL": true,
		"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
		"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
	}
	if reserved[baseUpper] {
		return true
	}
	for _, c := range name {
		switch c {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return true
		}
	}
	return strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ")
}

func sanitizeUnsafeName(name string) string {
	runes := []rune(name)
	for i, c := range runes {
		switch c {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			runes[i] = '_'
		}
	}
	for i := len(runes) - 1; i >= 0; i-- {
		if runes[i] == '.' || runes[i] == ' ' {
			runes[i] = '_'
			continue
		}
		break
	}
	return string(runes)
}

func percentEncodeName(name string) string {
	var b strings.Builder
	for _, ch := range []byte(name) {
		b.WriteString(fmt.Sprintf("%%%02x", ch))
	}
	return b.String()
}

func writeUaeMetadataForEntry(dirPath, amigaName, mappedName, entryType, mode string) error {
	switch mode {
	case "uaefsdb":
		uaeFsDbPath := filepath.Join(dirPath, "_UAEFSDB.___")
		nodes := make([]uaeFsDbNode, 0)
		existing, err := os.ReadFile(uaeFsDbPath)
		if err == nil && len(existing) > 0 {
			_ = json.Unmarshal(existing, &nodes)
		}
		replaced := false
		for i := range nodes {
			if nodes[i].AmigaName == amigaName {
				nodes[i] = uaeFsDbNode{AmigaName: amigaName, NormalName: mappedName, Type: entryType}
				replaced = true
				break
			}
		}
		if !replaced {
			nodes = append(nodes, uaeFsDbNode{AmigaName: amigaName, NormalName: mappedName, Type: entryType})
		}
		payload, err := json.MarshalIndent(nodes, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(uaeFsDbPath, payload, 0o644)
	case "uaemetafile":
		uaemPath := filepath.Join(dirPath, mappedName+".uaem")
		content := fmt.Sprintf("amiga_name=%s\nnormal_name=%s\ntype=%s\n", amigaName, mappedName, entryType)
		return os.WriteFile(uaemPath, []byte(content), 0o644)
	default:
		return nil
	}
}

func createBlankFile(path string, size int64) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Truncate(size)
}

func metadataFilePath(mediaPath string) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cacheDir, "hst-imager-go", "metadata")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	hash := fmt.Sprintf("%x", sha1.Sum([]byte(mediaPath)))
	return filepath.Join(dir, hash+".json"), nil
}

func loadMetadata(mediaPath string) (MediaMetadata, error) {
	metaPath, err := metadataFilePath(mediaPath)
	if err != nil {
		return MediaMetadata{}, err
	}
	b, err := os.ReadFile(metaPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return MediaMetadata{MediaPath: mediaPath}, nil
		}
		return MediaMetadata{}, err
	}
	var m MediaMetadata
	if err := json.Unmarshal(b, &m); err != nil {
		return MediaMetadata{}, err
	}
	if m.MediaPath == "" {
		m.MediaPath = mediaPath
	}
	return m, nil
}

func saveMetadata(mediaPath string, meta MediaMetadata) error {
	meta.MediaPath = mediaPath
	metaPath, err := metadataFilePath(mediaPath)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, b, 0o644)
}

func nextPartIndex(parts []Part) int {
	max := 0
	for _, p := range parts {
		if p.Index > max {
			max = p.Index
		}
	}
	return max + 1
}

func nextFsIndex(filesystems []RdbFileSystem) int {
	max := 0
	for _, fs := range filesystems {
		if fs.Index > max {
			max = fs.Index
		}
	}
	return max + 1
}

func usedBytes(parts []Part) int64 {
	var used int64
	for _, p := range parts {
		if p.Start+p.Size > used {
			used = p.Start + p.Size
		}
	}
	return used
}

func mediaSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func resolvePartSize(mediaPath, value string, parts []Part) (int64, error) {
	if value != "*" {
		return parseSize(value)
	}
	total := mediaSize(mediaPath)
	if total <= 0 {
		return 0, errors.New("cannot resolve '*' size without media file size")
	}
	remaining := total - usedBytes(parts)
	if remaining < 0 {
		remaining = 0
	}
	return remaining, nil
}

func findPart(parts []Part, index int) (*Part, error) {
	for i := range parts {
		if parts[i].Index == index {
			return &parts[i], nil
		}
	}
	return nil, fmt.Errorf("partition %d not found", index)
}

func copyRange(srcPath, dstPath string, srcOffset, dstOffset, size int64) (int64, error) {
	if size < 0 {
		return 0, errors.New("size must be >= 0")
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return 0, err
	}
	defer src.Close()
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil && filepath.Dir(dstPath) != "." {
		return 0, err
	}
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return 0, err
	}
	defer dst.Close()
	if _, err := src.Seek(srcOffset, io.SeekStart); err != nil {
		return 0, err
	}
	if _, err := dst.Seek(dstOffset, io.SeekStart); err != nil {
		return 0, err
	}
	written, err := io.CopyN(dst, src, size)
	if err != nil && !errors.Is(err, io.EOF) {
		return written, err
	}
	return written, nil
}

type readCloser struct {
	reader io.Reader
	closer io.Closer
}

func (r *readCloser) Read(p []byte) (int, error) { return r.reader.Read(p) }
func (r *readCloser) Close() error               { return r.closer.Close() }

type writeCloser struct {
	writer io.Writer
	close  func() error
}

func (w *writeCloser) Write(p []byte) (int, error) { return w.writer.Write(p) }
func (w *writeCloser) Close() error                { return w.close() }

type partitionRegion struct {
	BasePath string
	Table    string
	Index    int
	Offset   int64
	Size     int64
}

type archiveEntry struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

func openSourceReader(path string) (io.ReadCloser, error) {
	if region, ok, err := resolvePartitionSelection(path); err != nil {
		return nil, err
	} else if ok {
		f, err := os.Open(region.BasePath)
		if err != nil {
			return nil, err
		}
		section := io.NewSectionReader(f, region.Offset, region.Size)
		return &readCloser{reader: section, closer: f}, nil
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".gz":
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		gr, err := gzip.NewReader(f)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		return &readCloser{
			reader: gr,
			closer: closerFunc(func() error {
				err1 := gr.Close()
				err2 := f.Close()
				if err1 != nil {
					return err1
				}
				return err2
			}),
		}, nil
	case ".zip":
		zr, err := zip.OpenReader(path)
		if err != nil {
			return nil, err
		}
		if len(zr.File) == 0 {
			_ = zr.Close()
			return nil, errors.New("zip archive has no entries")
		}
		var picked *zip.File
		for _, f := range zr.File {
			if !f.FileInfo().IsDir() {
				picked = f
				break
			}
		}
		if picked == nil {
			_ = zr.Close()
			return nil, errors.New("zip archive has no file entries")
		}
		rc, err := picked.Open()
		if err != nil {
			_ = zr.Close()
			return nil, err
		}
		return &readCloser{
			reader: rc,
			closer: closerFunc(func() error {
				err1 := rc.Close()
				err2 := zr.Close()
				if err1 != nil {
					return err1
				}
				return err2
			}),
		}, nil
	default:
		return os.Open(path)
	}
}

func openDestinationWriter(path string) (io.WriteCloser, error) {
	if region, ok, err := resolvePartitionSelection(path); err != nil {
		return nil, err
	} else if ok {
		f, err := os.OpenFile(region.BasePath, os.O_RDWR, 0o644)
		if err != nil {
			return nil, err
		}
		if _, err := f.Seek(region.Offset, io.SeekStart); err != nil {
			_ = f.Close()
			return nil, err
		}
		return &partitionWriteCloser{f: f, remaining: region.Size}, nil
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".gz":
		f, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		gw := gzip.NewWriter(f)
		return &writeCloser{
			writer: gw,
			close: func() error {
				err1 := gw.Close()
				err2 := f.Close()
				if err1 != nil {
					return err1
				}
				return err2
			},
		}, nil
	case ".zip":
		f, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		zw := zip.NewWriter(f)
		entryName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if entryName == "" {
			entryName = "data.img"
		}
		w, err := zw.Create(entryName)
		if err != nil {
			_ = zw.Close()
			_ = f.Close()
			return nil, err
		}
		return &writeCloser{
			writer: w,
			close: func() error {
				err1 := zw.Close()
				err2 := f.Close()
				if err1 != nil {
					return err1
				}
				return err2
			},
		}, nil
	default:
		return os.Create(path)
	}
}

type closerFunc func() error

func (c closerFunc) Close() error { return c() }

type partitionWriteCloser struct {
	f         *os.File
	remaining int64
}

func (w *partitionWriteCloser) Write(p []byte) (int, error) {
	if w.remaining <= 0 {
		return 0, io.ErrShortWrite
	}
	if int64(len(p)) > w.remaining {
		p = p[:w.remaining]
	}
	n, err := w.f.Write(p)
	w.remaining -= int64(n)
	if err != nil {
		return n, err
	}
	return n, nil
}

func (w *partitionWriteCloser) Close() error { return w.f.Close() }

func resolvePartitionSelection(path string) (partitionRegion, bool, error) {
	basePath, table, index, ok := parsePartitionPath(path)
	if !ok {
		return partitionRegion{}, false, nil
	}
	switch table {
	case "mbr":
		parts, err := readMbrPartitions(basePath)
		if err != nil {
			return partitionRegion{}, false, err
		}
		p, err := findMbrPart(parts, index)
		if err != nil {
			return partitionRegion{}, false, err
		}
		return partitionRegion{
			BasePath: basePath,
			Table:    table,
			Index:    index,
			Offset:   int64(p.StartLBA) * mbrSectorSize,
			Size:     int64(p.SectorCount) * mbrSectorSize,
		}, true, nil
	case "gpt":
		_, parts, err := readGpt(basePath)
		if err != nil {
			return partitionRegion{}, false, err
		}
		p, err := findGptPart(parts, index)
		if err != nil {
			return partitionRegion{}, false, err
		}
		return partitionRegion{
			BasePath: basePath,
			Table:    table,
			Index:    index,
			Offset:   int64(p.FirstLBA) * mbrSectorSize,
			Size:     int64(p.LastLBA-p.FirstLBA+1) * mbrSectorSize,
		}, true, nil
	case "rdb":
		state, err := readRdbState(basePath)
		if err != nil {
			return partitionRegion{}, false, err
		}
		p, err := findRdbPart(state.Parts, index)
		if err != nil {
			return partitionRegion{}, false, err
		}
		return partitionRegion{
			BasePath: basePath,
			Table:    table,
			Index:    index,
			Offset:   p.Start,
			Size:     p.Size,
		}, true, nil
	default:
		return partitionRegion{}, false, fmt.Errorf("unsupported partition table selector: %s", table)
	}
}

func parsePartitionPath(path string) (basePath string, table string, index int, ok bool) {
	lower := strings.ToLower(path)
	for _, t := range []string{"mbr", "gpt", "rdb"} {
		suffix := `\` + t + `\`
		pos := strings.LastIndex(lower, suffix)
		if pos <= 0 {
			continue
		}
		idxStr := path[pos+len(suffix):]
		n, err := strconv.Atoi(idxStr)
		if err != nil || n < 1 {
			continue
		}
		return path[:pos], t, n, true
	}
	return "", "", 0, false
}

func parsePartitionContainerPath(path string) (basePath string, table string, ok bool) {
	lower := strings.ToLower(path)
	for _, t := range []string{"mbr", "gpt", "rdb"} {
		suffix := `\` + t
		if !strings.HasSuffix(lower, suffix) {
			continue
		}
		base := path[:len(path)-len(suffix)]
		if base == "" {
			continue
		}
		return base, t, true
	}
	return "", "", false
}

func findGptPart(parts []gptPartitionEntry, index int) (gptPartitionEntry, error) {
	for _, p := range parts {
		if p.Index == index {
			return p, nil
		}
	}
	return gptPartitionEntry{}, fmt.Errorf("partition %d not found", index)
}

func listArchiveEntries(path string) ([]archiveEntry, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".zip" {
		zr, err := zip.OpenReader(path)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		items := make([]archiveEntry, 0, len(zr.File))
		for _, f := range zr.File {
			items = append(items, archiveEntry{Name: f.Name, Size: int64(f.UncompressedSize64)})
		}
		return items, nil
	}
	if _, err := exec.LookPath("bsdtar"); err != nil {
		return nil, fmt.Errorf("archive format '%s' requires bsdtar: %w", ext, err)
	}
	out, err := exec.Command("bsdtar", "-tf", path).Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	items := make([]archiveEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		items = append(items, archiveEntry{Name: line, Size: 0})
	}
	return items, nil
}

func splitArchivePath(path string) (archivePath string, innerPath string, ok bool) {
	lower := strings.ToLower(path)
	for _, ext := range []string{".zip", ".lha", ".lzx", ".tar", ".gz", ".xz", ".bz2", ".rar", ".z"} {
		pos := strings.Index(lower, ext)
		if pos < 0 {
			continue
		}
		end := pos + len(ext)
		if end == len(path) {
			return path, "", true
		}
		next := path[end]
		if next == '\\' || next == '/' {
			return path[:end], strings.Trim(path[end+1:], `\/`), true
		}
	}
	return "", "", false
}

func normalizeArchivePath(path string) string {
	return strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/")
}

func extractArchive(archivePath, innerPath, destination string) error {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(archivePath))
	if ext == ".zip" {
		return extractZip(archivePath, innerPath, destination)
	}
	if _, err := exec.LookPath("bsdtar"); err != nil {
		return fmt.Errorf("archive extract for '%s' requires bsdtar: %w", ext, err)
	}
	args := []string{"-xf", archivePath, "-C", destination}
	if innerPath != "" {
		args = append(args, strings.ReplaceAll(innerPath, "\\", "/"))
	}
	cmd := exec.Command("bsdtar", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("archive extract failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func extractZip(archivePath, innerPath, destination string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()
	prefix := normalizeArchivePath(innerPath)
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	for _, f := range zr.File {
		name := normalizeArchivePath(f.Name)
		if prefix != "" {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			name = strings.TrimPrefix(name, prefix)
			name = strings.TrimPrefix(name, "/")
		}
		if name == "" {
			continue
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			_ = rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		closeErr1 := out.Close()
		closeErr2 := rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr1 != nil {
			return closeErr1
		}
		if closeErr2 != nil {
			return closeErr2
		}
	}
	return nil
}
