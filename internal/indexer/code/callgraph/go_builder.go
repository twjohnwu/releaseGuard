package callgraph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/token"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

type GoBuilder struct{}

func NewGoBuilder() *GoBuilder { return &GoBuilder{} }

func (b *GoBuilder) Build(ctx context.Context, repoPath string) (*BuildResult, error) {
	cfg := &packages.Config{
		Mode:    packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedDeps | packages.NeedFiles | packages.NeedName | packages.NeedCompiledGoFiles | packages.NeedImports,
		Dir:     repoPath,
		Context: ctx,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	prog, ssaPkgs := ssautil.AllPackages(pkgs, ssa.GlobalDebug)
	prog.Build()
	_ = ssaPkgs

	cg := cha.CallGraph(prog)
	cg.DeleteSyntheticNodes()

	out := &BuildResult{}
	added := map[string]bool{}

	absRepo, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("resolve repository path: %w", err)
	}
	for _, n := range cg.Nodes {
		if n == nil || n.Func == nil {
			continue
		}
		sym := nodeToSymbol(n, prog.Fset, absRepo)
		if sym == nil {
			continue
		}
		if !added[sym.ID] {
			out.Symbols = append(out.Symbols, *sym)
			added[sym.ID] = true
		}
		for _, edge := range n.Out {
			callee := nodeToSymbol(edge.Callee, prog.Fset, absRepo)
			if callee == nil {
				continue
			}
			if !added[callee.ID] {
				out.Symbols = append(out.Symbols, *callee)
				added[callee.ID] = true
			}
			var pos = prog.Fset.Position(edge.Pos())
			kind := "direct"
			if edge.Site != nil && edge.Site.Common().IsInvoke() {
				kind = "interface"
			}
			out.Edges = append(out.Edges, Edge{
				Caller:   sym.ID,
				Callee:   callee.ID,
				CallFile: pos.Filename,
				CallLine: pos.Line,
				Kind:     kind,
			})
		}
	}
	return out, nil
}

func nodeToSymbol(n *callgraph.Node, fset *token.FileSet, absRepo string) *Symbol {
	if n == nil || n.Func == nil || n.Func.Pkg == nil {
		return nil
	}
	pkgPath := n.Func.Pkg.Pkg.Path()
	id := pkgPath + "." + n.Func.Name()
	hash := sha256.Sum256([]byte(id))

	var relFile string
	var lineStart, lineEnd int
	if fset != nil && n.Func.Pos().IsValid() {
		startPos := fset.Position(n.Func.Pos())
		endPos := fset.Position(n.Func.Syntax().End())
		lineStart = startPos.Line
		lineEnd = endPos.Line
		if absRepo != "" && strings.HasPrefix(startPos.Filename, absRepo) {
			relFile = strings.TrimPrefix(startPos.Filename, absRepo+"/")
		} else {
			relFile = startPos.Filename
		}
	}

	return &Symbol{
		ID:        id,
		Kind:      "function",
		Language:  "go",
		File:      relFile,
		LineStart: lineStart,
		LineEnd:   lineEnd,
		Signature: n.Func.String(),
		Hash:      hex.EncodeToString(hash[:8]),
	}
}
