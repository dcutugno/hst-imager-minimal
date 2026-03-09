package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var optionLiteralRe = regexp.MustCompile(`"--[a-zA-Z0-9-]+"|"-[a-zA-Z0-9][a-zA-Z0-9-]*"`)
var aliasLiteralRe = regexp.MustCompile(`AddAlias\("([a-zA-Z0-9]+)"\)`)
var quotedWordRe = regexp.MustCompile(`"[a-zA-Z0-9-]+"`)

func TestCliShapeParityWithDotNetFactories(t *testing.T) {
	dotnetOptionTokens, dotnetAliasTokens, err := readDotNetCliTokens()
	if err != nil {
		t.Fatalf("read .NET CLI tokens failed: %v", err)
	}

	goSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read go source failed: %v", err)
	}
	goText := strings.ToLower(string(goSource))
	goOptionTokens := extractOptionTokens(goText)

	missingOptions := setDiff(dotnetOptionTokens, goOptionTokens)
	if len(missingOptions) > 0 {
		t.Fatalf("go CLI is missing option token(s) from .NET factories: %s", strings.Join(missingOptions, ", "))
	}

	aliasSection := goText
	if start := strings.Index(goText, "func normalizecommandtokenalias"); start >= 0 {
		aliasSection = goText[start:]
	}
	if end := strings.Index(aliasSection, "func normalizeoptionassignments"); end >= 0 {
		aliasSection = aliasSection[:end]
	}
	goAliasTokens := extractQuotedWords(aliasSection)
	missingAliases := setDiff(dotnetAliasTokens, goAliasTokens)
	if len(missingAliases) > 0 {
		t.Fatalf("go CLI is missing alias token(s) from .NET factories: %s", strings.Join(missingAliases, ", "))
	}
}

func TestCliAliasResolutionMatrixFromDotNetAliases(t *testing.T) {
	_, dotnetAliasTokens, err := readDotNetCliTokens()
	if err != nil {
		t.Fatalf("read .NET CLI alias tokens failed: %v", err)
	}
	root := BuildRootCommand()
	paths := collectCommandPaths(root)
	if len(paths) == 0 {
		t.Fatal("no command paths discovered from command tree")
	}

	totalVariants := 0
	for _, path := range paths {
		canonicalPath := resolveCommandPath(root, path)
		if canonicalPath == "" {
			continue
		}
		parent := root
		for i, token := range path {
			for alias := range dotnetAliasTokens {
				normalized := normalizeCommandTokenAlias(parent, strings.ToLower(alias))
				if normalized != token || alias == token {
					continue
				}
				variant := append([]string(nil), path...)
				variant[i] = alias
				resolvedVariant := resolveCommandPath(root, variant)
				if resolvedVariant != canonicalPath {
					t.Fatalf("alias variant did not resolve to canonical path: canonical=%q alias=%q variant=%v resolved=%q", canonicalPath, alias, variant, resolvedVariant)
				}
				totalVariants++
			}
			next := parent.Find(token)
			if next == nil {
				break
			}
			parent = next
		}
	}
	if totalVariants == 0 {
		t.Fatal("alias matrix generated zero variants")
	}
}

func TestCliOptionAssignmentMatrixFromDotNetOptions(t *testing.T) {
	dotnetOptionTokens, _, err := readDotNetCliTokens()
	if err != nil {
		t.Fatalf("read .NET CLI option tokens failed: %v", err)
	}
	total := 0
	for token := range dotnetOptionTokens {
		switch {
		case strings.HasPrefix(token, "--"):
			value := "value" + strconv.Itoa(total+1)
			got := normalizeOptionAssignments([]string{token + "=" + value})
			if len(got) != 2 || got[0] != token || got[1] != value {
				t.Fatalf("long option assignment split failed for %q: got=%v", token, got)
			}
			total++
		case strings.HasPrefix(token, "-"):
			value := "value" + strconv.Itoa(total+1)
			got := normalizeOptionAssignments([]string{token + "=" + value})
			if len(got) != 2 || got[0] != token || got[1] != value {
				t.Fatalf("short option assignment split failed for %q: got=%v", token, got)
			}
			total++
		}
	}
	if total == 0 {
		t.Fatal("option assignment matrix generated zero cases")
	}
}

func readDotNetCliTokens() (map[string]struct{}, map[string]struct{}, error) {
	base := filepath.Join("..", "Hst.Imager.ConsoleApp")
	patterns := []string{
		filepath.Join(base, "*CommandFactory.cs"),
		filepath.Join(base, "Commands", "*CommandFactory.cs"),
	}
	files := make([]string, 0, 32)
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, nil, err
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		return nil, nil, os.ErrNotExist
	}

	options := make(map[string]struct{}, 128)
	aliases := make(map[string]struct{}, 32)
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, err
		}
		content := string(b)
		for _, token := range optionLiteralRe.FindAllString(content, -1) {
			options[strings.ToLower(strings.Trim(token, `"`))] = struct{}{}
		}
		for _, match := range aliasLiteralRe.FindAllStringSubmatch(content, -1) {
			if len(match) < 2 {
				continue
			}
			aliases[strings.ToLower(strings.TrimSpace(match[1]))] = struct{}{}
		}
	}
	return options, aliases, nil
}

func extractOptionTokens(content string) map[string]struct{} {
	out := make(map[string]struct{}, 128)
	for _, token := range optionLiteralRe.FindAllString(content, -1) {
		out[strings.ToLower(strings.Trim(token, `"`))] = struct{}{}
	}
	return out
}

func extractQuotedWords(content string) map[string]struct{} {
	out := make(map[string]struct{}, 128)
	for _, token := range quotedWordRe.FindAllString(content, -1) {
		out[strings.ToLower(strings.Trim(token, `"`))] = struct{}{}
	}
	return out
}

func setDiff(left, right map[string]struct{}) []string {
	missing := make([]string, 0)
	for key := range left {
		if _, ok := right[key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}

func collectCommandPaths(root *Command) [][]string {
	paths := make([][]string, 0, 64)
	var walk func(cmd *Command, prefix []string)
	walk = func(cmd *Command, prefix []string) {
		for _, child := range cmd.Children {
			path := append(append([]string(nil), prefix...), child.Name)
			paths = append(paths, path)
			walk(child, path)
		}
	}
	walk(root, nil)
	return paths
}

func resolveCommandPath(root *Command, tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	normalized := normalizeCommandTokens(tokens, root)
	cmd, remaining := ResolveCommand(root, normalized)
	consumed := len(normalized) - len(remaining)
	if consumed == 0 || cmd == nil {
		return ""
	}
	return strings.Join(normalized[:consumed], " ")
}
