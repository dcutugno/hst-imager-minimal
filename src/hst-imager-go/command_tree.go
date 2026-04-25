package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

type Command struct {
	Name        string
	Description string
	Children    []*Command
}

func (c *Command) Find(name string) *Command {
	for _, child := range c.Children {
		if child.Name == name {
			return child
		}
	}
	return nil
}

func (c *Command) UsagePath() string {
	return c.Name
}

func BuildRootCommand() *Command {
	root := &Command{Name: "hst-imager", Description: "Read, write and initialize image files and physical disks."}

	root.Children = []*Command{
		{Name: "blank", Description: "Create a blank image file."},
		{Name: "convert", Description: "Convert an image file (legacy alias of transfer)."},
		{Name: "transfer", Description: "Transfer image data between sources and destinations."},
		{Name: "format", Description: "Format a physical disk or image file."},
		{Name: "info", Description: "Display information about an image file or physical disk."},
		{Name: "list", Description: "Display list of physical disks."},
		{Name: "optimize", Description: "Optimize an image file size."},
		{Name: "read", Description: "Read a physical disk to an image file."},
		{Name: "script", Description: "Run a script."},
		buildBlockCommand(),
		{Name: "compare", Description: "Compare image files and physical disks byte by byte."},
		{Name: "write", Description: "Write an image file to a physical disk."},
		buildGptCommand(),
		buildMbrCommand(),
		buildRdbCommand(),
		buildFsCommand(),
		buildAdfCommand(),
		buildArchiveCommand(),
		buildSettingsCommand(),
	}

	sort.Slice(root.Children, func(i, j int) bool {
		return root.Children[i].Name < root.Children[j].Name
	})

	return root
}

func buildBlockCommand() *Command {
	return &Command{Name: "block", Description: "Block operations.", Children: []*Command{
		{Name: "read", Description: "Read blocks from a disk/image into files."},
		{Name: "view", Description: "View blocks from a disk/image as hex."},
	}}
}

func buildGptCommand() *Command {
	return &Command{Name: "gpt", Description: "GUID partition table operations.", Children: []*Command{
		{Name: "info", Description: "Display GPT information."},
		{Name: "initialize", Description: "Initialize disk with empty GPT."},
		{Name: "part", Description: "GPT partition operations.", Children: []*Command{
			{Name: "add", Description: "Add partition."},
			{Name: "delete", Description: "Delete partition."},
			{Name: "format", Description: "Format partition."},
		}},
	}}
}

func buildMbrCommand() *Command {
	return &Command{Name: "mbr", Description: "Master boot record operations.", Children: []*Command{
		{Name: "info", Description: "Display MBR information."},
		{Name: "initialize", Description: "Initialize disk with empty MBR."},
		{Name: "part", Description: "MBR partition operations.", Children: []*Command{
			{Name: "add", Description: "Add partition."},
			{Name: "delete", Description: "Delete partition."},
			{Name: "format", Description: "Format partition."},
			{Name: "export", Description: "Export partition to file."},
			{Name: "import", Description: "Import partition from file."},
			{Name: "clone", Description: "Clone partition from source."},
		}},
	}}
}

func buildRdbCommand() *Command {
	return &Command{Name: "rdb", Description: "Rigid disk block operations.", Children: []*Command{
		{Name: "info", Description: "Display RDB information."},
		{Name: "initialize", Description: "Initialize disk with empty RDB."},
		{Name: "resize", Description: "Resize RDB."},
		{Name: "filesystem", Description: "RDB file system operations.", Children: []*Command{
			{Name: "add", Description: "Add file system."},
			{Name: "delete", Description: "Delete file system."},
			{Name: "import", Description: "Import file system."},
			{Name: "export", Description: "Export file system."},
			{Name: "update", Description: "Update file system."},
		}},
		{Name: "part", Description: "RDB partition operations.", Children: []*Command{
			{Name: "add", Description: "Add partition."},
			{Name: "update", Description: "Update partition."},
			{Name: "delete", Description: "Delete partition."},
			{Name: "copy", Description: "Copy partition."},
			{Name: "export", Description: "Export partition."},
			{Name: "import", Description: "Import partition."},
			{Name: "kill", Description: "Kill partition."},
			{Name: "move", Description: "Move partition."},
			{Name: "format", Description: "Format partition."},
		}},
		{Name: "update", Description: "Update RDB."},
		{Name: "backup", Description: "Backup RDB to file."},
		{Name: "restore", Description: "Restore RDB from file."},
	}}
}

func buildFsCommand() *Command {
	return &Command{Name: "fs", Description: "File system operations.", Children: []*Command{
		{Name: "dir", Description: "List files and subdirectories."},
		{Name: "copy", Description: "Copy files or subdirectories."},
		{Name: "extract", Description: "Extract files or subdirectories."},
		{Name: "mkdir", Description: "Create directory."},
		{Name: "mklink", Description: "Create Amiga filesystem link."},
		{Name: "delete", Description: "Delete local files or directories."},
		{Name: "rename", Description: "Rename local files or directories."},
	}}
}

func buildAdfCommand() *Command {
	return &Command{Name: "adf", Description: "Amiga disk file operations.", Children: []*Command{
		{Name: "create", Description: "Create ADF disk image file."},
	}}
}

func buildArchiveCommand() *Command {
	return &Command{Name: "archive", Description: "Archive operations.", Children: []*Command{
		{Name: "list", Description: "List files and subdirectories in archive."},
	}}
}

func buildSettingsCommand() *Command {
	return &Command{Name: "settings", Description: "Settings operations.", Children: []*Command{
		{Name: "list", Description: "List settings."},
		{Name: "update", Description: "Update settings."},
	}}
}

func PrintHelp(w io.Writer, cmd *Command, parentPath string) {
	path := cmd.Name
	if parentPath != "" {
		path = parentPath + " " + cmd.Name
	}

	fmt.Fprintf(w, "%s\n\n", cmd.Description)
	fmt.Fprintf(w, "Usage:\n  %s [command]\n\n", path)
	if len(cmd.Children) == 0 {
		return
	}
	fmt.Fprintln(w, "Commands:")
	for _, child := range cmd.Children {
		fmt.Fprintf(w, "  %-14s %s\n", child.Name, child.Description)
	}
}

func ResolveCommand(root *Command, args []string) (*Command, []string) {
	if len(args) == 0 {
		return root, nil
	}
	current := root
	consumed := 0
	for consumed < len(args) {
		next := current.Find(strings.ToLower(args[consumed]))
		if next == nil {
			break
		}
		current = next
		consumed++
	}
	return current, args[consumed:]
}
