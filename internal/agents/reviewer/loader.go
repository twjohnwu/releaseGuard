package reviewer

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func loadMDDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return nil, err
		}
		out = append(out, string(b))
	}
	return out, nil
}

// LoadPromptFiles reads, in order:
//  1. <projectsDir>/_shared/*.md
//  2. for each serviceType: <projectsDir>/_shared/<type>/*.md
//  3. <projectsDir>/<systemName>/*.md
func LoadPromptFiles(projectsDir, systemName string, serviceTypes []string) ([]string, error) {
	var all []string
	shared, err := loadMDDir(filepath.Join(projectsDir, "_shared"))
	if err != nil {
		return nil, err
	}
	all = append(all, shared...)
	for _, st := range serviceTypes {
		typed, err := loadMDDir(filepath.Join(projectsDir, "_shared", st))
		if err != nil {
			return nil, err
		}
		all = append(all, typed...)
	}
	if systemName != "" {
		sys, err := loadMDDir(filepath.Join(projectsDir, systemName))
		if err != nil {
			return nil, err
		}
		all = append(all, sys...)
	}
	return all, nil
}
