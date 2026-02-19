package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	case "info":
		return handleInfo(remaining, stdout, opts)
	case "transfer", "read", "write":
		return handleTransfer(remaining, stdout, consumedPath, opts)
	case "compare":
		return handleCompare(remaining, stdout, opts)
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
