package main

import (
	"archive/zip"
	"bufio"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
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

type mbrPartition struct {
	Index       int
	Bootable    bool
	TypeCode    byte
	StartLBA    uint32
	SectorCount uint32
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

func handleArchiveList(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 1 {
		return errors.New("usage: archive list <path-to-zip>")
	}
	zr, err := zip.OpenReader(args[0])
	if err != nil {
		return err
	}
	defer zr.Close()
	type entry struct {
		Name string `json:"name"`
		Size uint64 `json:"size"`
	}
	items := make([]entry, 0, len(zr.File))
	for _, f := range zr.File {
		items = append(items, entry{Name: f.Name, Size: f.UncompressedSize64})
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
	info, err := os.Stat(args[0])
	if err != nil {
		return err
	}
	fType := fileType(info)
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"path": args[0], "type": fType, "size": info.Size()})
	}
	fmt.Fprintf(stdout, "Path: %s\n", args[0])
	fmt.Fprintf(stdout, "Type: %s\n", fType)
	fmt.Fprintf(stdout, "Size: %d\n", info.Size())
	return nil
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
	src, err := os.Open(source)
	if err != nil {
		return 0, err
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil && filepath.Dir(destination) != "." {
		return 0, err
	}
	dst, err := os.Create(destination)
	if err != nil {
		return 0, err
	}
	defer dst.Close()

	if size > 0 {
		return io.CopyN(dst, src, size)
	}
	return io.Copy(dst, src)
}

func compareFiles(source, destination string, size int64) (bool, int64, error) {
	a, err := os.ReadFile(source)
	if err != nil {
		return false, 0, err
	}
	b, err := os.ReadFile(destination)
	if err != nil {
		return false, 0, err
	}

	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	if size > 0 && int(size) < limit {
		limit = int(size)
	}

	for i := 0; i < limit; i++ {
		if a[i] != b[i] {
			return false, int64(i + 1), nil
		}
	}
	if size == 0 && len(a) != len(b) {
		return false, int64(limit), nil
	}
	return true, int64(limit), nil
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
		meta, err := loadMetadata(path)
		if err != nil {
			return err
		}
		if meta.RdbSize <= 0 {
			return errors.New("no RDB size present for media")
		}
		target = meta.RdbSize
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
	meta, err := loadMetadata(path)
	if err != nil {
		return err
	}
	switch formatType {
	case "mbr":
		meta.MbrParts = nil
	case "gpt":
		meta.GptParts = nil
	case "rdb":
		meta.RdbParts = nil
		meta.RdbFs = nil
		meta.RdbSize = 0
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
	if formatType != "adf" {
		if err := saveMetadata(path, meta); err != nil {
			return err
		}
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
		return errors.New("usage: fs copy <source> <destination> [--recursive]")
	}
	recursive := false
	if len(args) > 2 {
		if len(args) == 3 && args[2] == "--recursive" {
			recursive = true
		} else {
			return fmt.Errorf("unsupported arguments: %s", strings.Join(args[2:], " "))
		}
	}
	count, err := copyPath(args[0], args[1], recursive)
	if err != nil {
		return err
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"source": args[0], "destination": args[1], "entries": count})
	}
	fmt.Fprintf(stdout, "Copied %d entries from '%s' to '%s'.\n", count, args[0], args[1])
	return nil
}

func handleFsExtract(args []string, stdout io.Writer, opts GlobalOptions) error {
	return handleFsCopy(args, stdout, opts)
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
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{"media": args[0], "parts": meta.GptParts})
	}
	if len(meta.GptParts) == 0 {
		fmt.Fprintln(stdout, "No GPT partitions.")
		return nil
	}
	for _, p := range meta.GptParts {
		fmt.Fprintf(stdout, "#%d type=%s start=%d size=%d name=%s\n", p.Index, p.Type, p.Start, p.Size, p.Name)
	}
	return nil
}

func handleGptInitialize(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 1 {
		return errors.New("usage: gpt initialize <media>")
	}
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	meta.GptParts = nil
	if err := saveMetadata(args[0], meta); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "gpt initialized", args[0])
}

func handleGptPartAdd(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 3 {
		return errors.New("usage: gpt part add <media> <type> <size|*>")
	}
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	size, err := resolvePartSize(args[0], args[2], meta.GptParts)
	if err != nil {
		return err
	}
	part := Part{Index: nextPartIndex(meta.GptParts), Type: args[1], Start: usedBytes(meta.GptParts), Size: size}
	meta.GptParts = append(meta.GptParts, part)
	if err := saveMetadata(args[0], meta); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "gpt partition added", part)
}

func handleGptPartDelete(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: gpt part delete <media> <index>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	parts := make([]Part, 0, len(meta.GptParts))
	found := false
	for _, p := range meta.GptParts {
		if p.Index == idx {
			found = true
			continue
		}
		parts = append(parts, p)
	}
	if !found {
		return fmt.Errorf("partition %d not found", idx)
	}
	meta.GptParts = parts
	if err := saveMetadata(args[0], meta); err != nil {
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
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	p, err := findPart(meta.GptParts, idx)
	if err != nil {
		return err
	}
	p.Name = args[2]
	if err := saveMetadata(args[0], meta); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "gpt partition formatted", p)
}

func handleRdbInfo(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 1 {
		return errors.New("usage: rdb info <media>")
	}
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	if opts.Format == "json" {
		return writeJSON(stdout, map[string]any{
			"media":       args[0],
			"rdbSize":     meta.RdbSize,
			"partitions":  meta.RdbParts,
			"filesystems": meta.RdbFs,
		})
	}
	fmt.Fprintf(stdout, "RDB size: %d\n", meta.RdbSize)
	for _, fs := range meta.RdbFs {
		fmt.Fprintf(stdout, "FS #%d path=%s dosType=%s version=%s\n", fs.Index, fs.Path, fs.DosType, fs.Version)
	}
	for _, p := range meta.RdbParts {
		fmt.Fprintf(stdout, "Part #%d name=%s type=%s start=%d size=%d status=%s\n", p.Index, p.Name, p.Type, p.Start, p.Size, p.Status)
	}
	return nil
}

func handleRdbInitialize(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 1 {
		return errors.New("usage: rdb initialize <media>")
	}
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	meta.RdbParts = nil
	meta.RdbFs = nil
	meta.RdbSize = mediaSize(args[0])
	if err := saveMetadata(args[0], meta); err != nil {
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
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	meta.RdbSize = size
	if err := saveMetadata(args[0], meta); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb resized", size)
}

func handleRdbFsAdd(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: rdb filesystem add <media> <path> [dostype]")
	}
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	fs := RdbFileSystem{Index: nextFsIndex(meta.RdbFs), Path: args[1]}
	if len(args) > 2 {
		fs.DosType = args[2]
	}
	meta.RdbFs = append(meta.RdbFs, fs)
	if err := saveMetadata(args[0], meta); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb filesystem added", fs)
}

func handleRdbFsDelete(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: rdb filesystem delete <media> <index>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	out := make([]RdbFileSystem, 0, len(meta.RdbFs))
	found := false
	for _, fs := range meta.RdbFs {
		if fs.Index == idx {
			found = true
			continue
		}
		out = append(out, fs)
	}
	if !found {
		return fmt.Errorf("filesystem %d not found", idx)
	}
	meta.RdbFs = out
	if err := saveMetadata(args[0], meta); err != nil {
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
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	var target *RdbFileSystem
	for i := range meta.RdbFs {
		if meta.RdbFs[i].Index == idx {
			target = &meta.RdbFs[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("filesystem %d not found", idx)
	}
	written, err := copyFile(target.Path, args[2], 0)
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
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	found := false
	for i := range meta.RdbFs {
		if meta.RdbFs[i].Index == idx {
			meta.RdbFs[i].DosType = args[2]
			if len(args) > 3 {
				meta.RdbFs[i].Version = args[3]
			}
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("filesystem %d not found", idx)
	}
	if err := saveMetadata(args[0], meta); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb filesystem updated", idx)
}

func handleRdbPartAdd(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 4 {
		return errors.New("usage: rdb part add <media> <name> <type> <size|*>")
	}
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	size, err := resolvePartSize(args[0], args[3], meta.RdbParts)
	if err != nil {
		return err
	}
	part := Part{
		Index:  nextPartIndex(meta.RdbParts),
		Name:   args[1],
		Type:   args[2],
		Start:  usedBytes(meta.RdbParts),
		Size:   size,
		Status: "active",
	}
	meta.RdbParts = append(meta.RdbParts, part)
	if err := saveMetadata(args[0], meta); err != nil {
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
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	p, err := findPart(meta.RdbParts, idx)
	if err != nil {
		return err
	}
	p.Name = args[2]
	if err := saveMetadata(args[0], meta); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb partition updated", p)
}

func handleRdbPartDelete(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: rdb part delete <media> <index>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	out := make([]Part, 0, len(meta.RdbParts))
	found := false
	for _, p := range meta.RdbParts {
		if p.Index == idx {
			found = true
			continue
		}
		out = append(out, p)
	}
	if !found {
		return fmt.Errorf("partition %d not found", idx)
	}
	meta.RdbParts = out
	if err := saveMetadata(args[0], meta); err != nil {
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
	srcMeta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	dstMeta, err := loadMetadata(args[2])
	if err != nil {
		return err
	}
	srcPart, err := findPart(srcMeta.RdbParts, srcIdx)
	if err != nil {
		return err
	}
	dstPart, err := findPart(dstMeta.RdbParts, dstIdx)
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
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	p, err := findPart(meta.RdbParts, idx)
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
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	p, err := findPart(meta.RdbParts, idx)
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
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	p, err := findPart(meta.RdbParts, idx)
	if err != nil {
		return err
	}
	p.Status = "killed"
	if err := saveMetadata(args[0], meta); err != nil {
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
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	p, err := findPart(meta.RdbParts, idx)
	if err != nil {
		return err
	}
	p.Start = start
	if err := saveMetadata(args[0], meta); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb partition moved", p)
}

func handleRdbPartFormat(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 3 {
		return errors.New("usage: rdb part format <media> <index> <label>")
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	p, err := findPart(meta.RdbParts, idx)
	if err != nil {
		return err
	}
	p.Name = args[2]
	if err := saveMetadata(args[0], meta); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb partition formatted", p)
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
	meta, err := loadMetadata(args[0])
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(args[1], b, 0o644); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb backup written", args[1])
}

func handleRdbRestore(args []string, stdout io.Writer, opts GlobalOptions) error {
	if len(args) < 2 {
		return errors.New("usage: rdb restore <media> <input>")
	}
	b, err := os.ReadFile(args[1])
	if err != nil {
		return err
	}
	var meta MediaMetadata
	if err := json.Unmarshal(b, &meta); err != nil {
		return err
	}
	meta.MediaPath = args[0]
	if err := saveMetadata(args[0], meta); err != nil {
		return err
	}
	return printSimpleStatus(stdout, opts, "rdb backup restored", args[0])
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
