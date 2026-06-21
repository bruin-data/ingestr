package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type lockFile struct {
	Version      int                `yaml:"version"`
	Generated    generatedMetadata  `yaml:"generated,omitempty"`
	ManualAudits []manualAudit      `yaml:"manual_audits,omitempty"`
	Dependencies []dependencyReview `yaml:"dependencies,omitempty"`
}

type generatedMetadata struct {
	Tool         string   `yaml:"tool,omitempty"`
	Packages     string   `yaml:"packages,omitempty"`
	Targets      []string `yaml:"targets,omitempty"`
	IncludeTests bool     `yaml:"include_tests"`
}

type manualAudit struct {
	Module        string `yaml:"module"`
	Version       string `yaml:"version"`
	License       string `yaml:"license"`
	LicenseFile   string `yaml:"license_file"`
	LicenseSHA256 string `yaml:"license_sha256"`
	Status        string `yaml:"status"`
	Note          string `yaml:"note,omitempty"`
}

type dependencyReview struct {
	Module   string   `yaml:"module"`
	Version  string   `yaml:"version"`
	Licenses []string `yaml:"licenses"`
	Status   string   `yaml:"status"`
	Note     string   `yaml:"note,omitempty"`
}

type moduleInfo struct {
	Path    string
	Version string
}

type scanEntry struct {
	Module   string
	Version  string
	Licenses []string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "licenseaudit: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		mode         string
		lockPath     string
		csvPath      string
		modulesPath  string
		outputPath   string
		newStatus    string
		tool         string
		packages     string
		targets      string
		includeTests bool
	)

	flag.StringVar(&mode, "mode", "check", "mode: check, write, ignored-modules, append-manual-notices")
	flag.StringVar(&lockPath, "lock", "licenses.lock.yml", "license audit lock file")
	flag.StringVar(&csvPath, "csv", "", "go-licenses csv output")
	flag.StringVar(&modulesPath, "modules", "", "go list -m all output")
	flag.StringVar(&outputPath, "output", "", "output file for append-manual-notices")
	flag.StringVar(&newStatus, "new-status", "needs-review", "fallback status for new or changed dependencies whose licenses are not default-allowed")
	flag.StringVar(&tool, "tool", "", "scanner tool metadata for write mode")
	flag.StringVar(&packages, "packages", "", "package pattern metadata for write mode")
	flag.StringVar(&targets, "targets", "", "space-separated target metadata for write mode")
	flag.BoolVar(&includeTests, "include-tests", false, "include test dependency metadata for write mode")
	flag.Parse()

	switch mode {
	case "ignored-modules":
		lock, err := readLock(lockPath, true)
		if err != nil {
			return err
		}
		for _, audit := range lock.ManualAudits {
			if audit.Module != "" {
				fmt.Println(audit.Module)
			}
		}
		return nil

	case "append-manual-notices":
		if outputPath == "" {
			return errors.New("-output is required for append-manual-notices")
		}
		lock, err := readLock(lockPath, false)
		if err != nil {
			return err
		}
		return appendManualNotices(lock.ManualAudits, outputPath)

	case "check", "write":
		if csvPath == "" || modulesPath == "" {
			return errors.New("-csv and -modules are required")
		}
		lock, err := readLock(lockPath, mode == "write")
		if err != nil {
			return err
		}
		modules, err := readModules(modulesPath)
		if err != nil {
			return err
		}
		scanned, err := readScan(csvPath, modules)
		if err != nil {
			return err
		}
		if mode == "check" {
			return checkLock(lock, scanned)
		}
		lock.Version = 1
		lock.Generated = generatedMetadata{
			Tool:         tool,
			Packages:     packages,
			Targets:      strings.Fields(targets),
			IncludeTests: includeTests,
		}
		lock.Dependencies = mergeDependencies(lock.Dependencies, scanned, newStatus)
		return writeLock(lockPath, lock)

	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
}

func readLock(path string, allowMissing bool) (*lockFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if allowMissing && errors.Is(err, os.ErrNotExist) {
			return &lockFile{Version: 1}, nil
		}
		return nil, err
	}
	var lock lockFile
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if lock.Version == 0 {
		lock.Version = 1
	}
	return &lock, nil
}

func writeLock(path string, lock *lockFile) error {
	sortManualAudits(lock.ManualAudits)
	sortDependencies(lock.Dependencies)

	data, err := yaml.Marshal(lock)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readModules(path string) ([]moduleInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var modules []moduleInfo
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		version := ""
		if len(fields) > 1 {
			version = fields[1]
		}
		modules = append(modules, moduleInfo{Path: fields[0], Version: version})
	}
	sort.Slice(modules, func(i, j int) bool {
		return len(modules[i].Path) > len(modules[j].Path)
	})
	return modules, nil
}

func readScan(path string, modules []moduleInfo) ([]scanEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	byModule := make(map[string]*scanEntry)
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = 3

	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		library := record[0]
		licenseName := record[2]
		mod, ok := findModule(library, modules)
		if !ok {
			return nil, fmt.Errorf("no module found for scanned library %q", library)
		}
		entry := byModule[mod.Path]
		if entry == nil {
			entry = &scanEntry{Module: mod.Path, Version: mod.Version}
			byModule[mod.Path] = entry
		}
		if !contains(entry.Licenses, licenseName) {
			entry.Licenses = append(entry.Licenses, licenseName)
		}
	}

	entries := make([]scanEntry, 0, len(byModule))
	for _, entry := range byModule {
		sort.Strings(entry.Licenses)
		entries = append(entries, *entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Module < entries[j].Module
	})
	return entries, nil
}

func findModule(library string, modules []moduleInfo) (moduleInfo, bool) {
	for _, mod := range modules {
		if library == mod.Path || strings.HasPrefix(library, mod.Path+"/") {
			return mod, true
		}
	}
	return moduleInfo{}, false
}

func checkLock(lock *lockFile, scanned []scanEntry) error {
	var problems []string

	deps := make(map[string]dependencyReview, len(lock.Dependencies))
	for _, dep := range lock.Dependencies {
		if dep.Module == "" {
			problems = append(problems, "lock contains a dependency with an empty module")
			continue
		}
		if _, exists := deps[dep.Module]; exists {
			problems = append(problems, fmt.Sprintf("lock contains duplicate dependency %s", dep.Module))
			continue
		}
		deps[dep.Module] = dep
		if !acceptedStatus(dep.Status) {
			problems = append(problems, fmt.Sprintf("%s has status %q; set an accepted status after review", dep.Module, dep.Status))
		}
	}

	seen := make(map[string]bool, len(scanned))
	for _, scan := range scanned {
		seen[scan.Module] = true
		dep, ok := deps[scan.Module]
		if !ok {
			problems = append(problems, fmt.Sprintf("new dependency %s@%s with licenses %s is missing from licenses.lock.yml", scan.Module, scan.Version, strings.Join(scan.Licenses, ", ")))
			continue
		}
		if dep.Version != scan.Version {
			problems = append(problems, fmt.Sprintf("%s version changed: lock has %s, scan has %s", scan.Module, dep.Version, scan.Version))
		}
		if !sameStrings(dep.Licenses, scan.Licenses) {
			problems = append(problems, fmt.Sprintf("%s licenses changed: lock has %s, scan has %s", scan.Module, strings.Join(dep.Licenses, ", "), strings.Join(scan.Licenses, ", ")))
		}
	}

	for _, dep := range lock.Dependencies {
		if !seen[dep.Module] {
			problems = append(problems, fmt.Sprintf("%s@%s is in licenses.lock.yml but was not found in the current scan", dep.Module, dep.Version))
		}
	}

	for _, audit := range lock.ManualAudits {
		if !acceptedStatus(audit.Status) {
			problems = append(problems, fmt.Sprintf("%s has manual audit status %q; set an accepted status after review", audit.Module, audit.Status))
		}
		if err := validateManualAudit(audit); err != nil {
			problems = append(problems, err.Error())
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		var b strings.Builder
		b.WriteString("license audit failed:\n")
		for _, problem := range problems {
			b.WriteString("  - ")
			b.WriteString(problem)
			b.WriteByte('\n')
		}
		return errors.New(strings.TrimRight(b.String(), "\n"))
	}

	fmt.Printf("license audit passed: %d dependencies, %d manual audits\n", len(scanned), len(lock.ManualAudits))
	return nil
}

func mergeDependencies(existing []dependencyReview, scanned []scanEntry, newStatus string) []dependencyReview {
	byModule := make(map[string]dependencyReview, len(existing))
	for _, dep := range existing {
		byModule[dep.Module] = dep
	}

	next := make([]dependencyReview, 0, len(scanned))
	for _, scan := range scanned {
		old, ok := byModule[scan.Module]
		status, note := defaultReviewStatus(scan, newStatus)
		if ok {
			if old.Version == scan.Version && sameStrings(old.Licenses, scan.Licenses) {
				status = old.Status
				note = old.Note
			} else if old.Status == "manual-review" {
				status = "needs-review"
				note = "Manual-reviewed dependency changed; review before setting status."
			} else {
				status, note = changedReviewStatus(scan, newStatus)
			}
		}
		next = append(next, dependencyReview{
			Module:   scan.Module,
			Version:  scan.Version,
			Licenses: scan.Licenses,
			Status:   status,
			Note:     note,
		})
	}
	sortDependencies(next)
	return next
}

func changedReviewStatus(scan scanEntry, fallbackStatus string) (string, string) {
	status, note := defaultReviewStatus(scan, fallbackStatus)
	if status == "needs-review" {
		note = "Version or license changed; review before setting status."
	}
	return status, note
}

func defaultReviewStatus(scan scanEntry, fallbackStatus string) (string, string) {
	if defaultAllowedLicenses(scan.Licenses) {
		return "allowed", ""
	}
	if fallbackStatus == "" {
		fallbackStatus = "needs-review"
	}
	if fallbackStatus == "needs-review" {
		return fallbackStatus, "License is not in the default allowlist; review before setting status."
	}
	return fallbackStatus, ""
}

func defaultAllowedLicenses(licenses []string) bool {
	if len(licenses) == 0 {
		return false
	}
	for _, license := range licenses {
		if !defaultAllowedLicense(license) {
			return false
		}
	}
	return true
}

func defaultAllowedLicense(license string) bool {
	switch license {
	case "0BSD",
		"Apache-2.0",
		"BSD-2-Clause",
		"BSD-3-Clause",
		"CC0-1.0",
		"ISC",
		"MIT",
		"MPL-2.0",
		"Unlicense",
		"Zlib":
		return true
	default:
		return false
	}
}

func appendManualNotices(audits []manualAudit, outputPath string) (err error) {
	out, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := out.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	for _, audit := range audits {
		if err := validateManualAudit(audit); err != nil {
			if errors.Is(err, errModuleNotSelected) {
				continue
			}
			return err
		}
		_, dir, ok, err := selectedModuleDir(audit)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		licensePath := filepath.Join(dir, audit.LicenseFile)
		if _, err := fmt.Fprintf(out, "\n===============================================================================\n%s/%s (%s, manually audited)\n===============================================================================\n\n", audit.Module, audit.LicenseFile, audit.License); err != nil {
			return err
		}
		if err := appendTrimmedFile(out, licensePath); err != nil {
			return err
		}
	}
	return nil
}

var errModuleNotSelected = errors.New("module is not selected")

func validateManualAudit(audit manualAudit) error {
	if audit.Module == "" || audit.Version == "" || audit.LicenseFile == "" || audit.LicenseSHA256 == "" {
		return fmt.Errorf("manual audit for %q is incomplete", audit.Module)
	}

	version, dir, ok, err := selectedModuleDir(audit)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", errModuleNotSelected, audit.Module)
	}
	if version != audit.Version {
		return fmt.Errorf("manual audit for %s is pinned to %s, but go.mod selects %s", audit.Module, audit.Version, version)
	}

	licensePath := filepath.Join(dir, audit.LicenseFile)
	hash, err := fileSHA256(licensePath)
	if err != nil {
		return fmt.Errorf("manual audit for %s expected %s: %w", audit.Module, audit.LicenseFile, err)
	}
	if hash != audit.LicenseSHA256 {
		return fmt.Errorf("manual audit for %s expected SHA-256 %s, found %s", audit.Module, audit.LicenseSHA256, hash)
	}
	return nil
}

func selectedModuleDir(audit manualAudit) (string, string, bool, error) {
	version, dir, ok, err := goListModule(audit.Module)
	if err != nil || !ok {
		return "", "", ok, err
	}
	if dir == "" || !fileExists(filepath.Join(dir, audit.LicenseFile)) {
		downloadedDir, err := goModDownloadDir(audit.Module, audit.Version)
		if err != nil {
			return "", "", true, err
		}
		if downloadedDir != "" {
			dir = downloadedDir
		}
	}
	return version, dir, true, nil
}

func goListModule(module string) (string, string, bool, error) {
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Version}}\t{{.Dir}}", module)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("go list -m %s: %w", module, err)
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
	version := parts[0]
	dir := ""
	if len(parts) > 1 {
		dir = parts[1]
	}
	return version, dir, true, nil
}

func goModDownloadDir(module, version string) (string, error) {
	cmd := exec.Command("go", "mod", "download", "-json", module+"@"+version)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	var result struct {
		Dir string
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", err
	}
	return result.Dir, nil
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func appendTrimmedFile(out io.Writer, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if i == len(lines)-1 && line == "" {
			break
		}
		if _, err := fmt.Fprintln(out, strings.TrimRight(line, " \t")); err != nil {
			return err
		}
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func acceptedStatus(status string) bool {
	switch status {
	case "allowed", "manual-review":
		return true
	default:
		return false
	}
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a = append([]string(nil), a...)
	b = append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sortManualAudits(audits []manualAudit) {
	sort.Slice(audits, func(i, j int) bool {
		return audits[i].Module < audits[j].Module
	})
}

func sortDependencies(deps []dependencyReview) {
	for i := range deps {
		sort.Strings(deps[i].Licenses)
	}
	sort.Slice(deps, func(i, j int) bool {
		return deps[i].Module < deps[j].Module
	})
}
