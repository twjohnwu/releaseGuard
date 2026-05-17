package schema

import (
	"path/filepath"
	"strings"

	"io/fs"
	"os"
)

type Located struct {
	OpenAPIs []string
	Protos   []string
}

func LocateInDir(root string) (*Located, error) {
	out := &Located{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if name == "openapi.yaml" || name == "openapi.yml" || name == "openapi.json" ||
			strings.HasSuffix(name, ".openapi.yaml") {
			out.OpenAPIs = append(out.OpenAPIs, p)
		}
		if strings.HasSuffix(name, ".proto") {
			out.Protos = append(out.Protos, p)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return out, nil
}
