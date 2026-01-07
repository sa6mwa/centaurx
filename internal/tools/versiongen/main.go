package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"pkt.systems/centaurx/internal/version"
)

func main() {
	var androidPath string
	flag.StringVar(&androidPath, "android", "", "path to Android build.gradle.kts to update versionName")
	flag.Parse()

	ver := strings.TrimSpace(version.Current())
	if ver == "" {
		ver = "v0.0.0-unknown"
	}

	if androidPath != "" {
		if err := updateAndroidVersionName(androidPath, ver); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	}

	fmt.Fprintln(os.Stdout, ver)
}

func updateAndroidVersionName(path string, ver string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat android file: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read android file: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	replaced := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "versionName") {
			continue
		}
		firstQuote := strings.IndexByte(line, '"')
		if firstQuote == -1 {
			return fmt.Errorf("android versionName line missing opening quote")
		}
		secondQuote := strings.IndexByte(line[firstQuote+1:], '"')
		if secondQuote == -1 {
			return fmt.Errorf("android versionName line missing closing quote")
		}
		secondQuote += firstQuote + 1
		lines[i] = line[:firstQuote+1] + ver + line[secondQuote:]
		replaced++
	}
	if replaced == 0 {
		return fmt.Errorf("android versionName not found in %s", path)
	}
	if replaced > 1 {
		return fmt.Errorf("android versionName appears %d times in %s", replaced, path)
	}
	out := strings.Join(lines, "\n")
	if len(data) > 0 && data[len(data)-1] == '\n' && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	if err := os.WriteFile(path, []byte(out), info.Mode().Perm()); err != nil {
		return fmt.Errorf("write android file: %w", err)
	}
	return nil
}
