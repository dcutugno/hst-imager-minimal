package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
