// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build ignore

// mkwasm does, for linux/wasm (mkall_linux_wasm.sh), what mkall.go and
// mkmerge do in the Docker build:
//
//	go run linux/mkwasm.go abi CC > include/abi/abi.h
//	go run linux/mkwasm.go merge zerrors_linux_wasm.go zerrors_linux.go ztypes_linux.go...
//	go run linux/mkwasm.go split zsyscall_linux.go zsyscall_linux_nowasm.go wait4 munmap...
//
// abi: the bit field masks of the C compiler CC (makeABIHeaders), as its
// little-endian ELF object has them. merge: drop from an architecture's file
// what the common files declare (constants, types, functions, variables).
// split: move functions out of a common file, into one for all but
// linux/wasm (which has its own).
package main

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	switch os.Args[1] {
	case "abi":
		abi(os.Args[2])
	case "merge":
		merge(os.Args[2], os.Args[3:])
	case "split":
		split(os.Args[2], os.Args[3], os.Args[4:])
	default:
		fail("usage: mkwasm abi CC | merge ARCH COMMON... | split COMMON OUT NAME...")
	}
}

func fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "mkwasm: "+f+"\n", a...)
	os.Exit(1)
}

func abi(cc string) {
	var src bytes.Buffer
	src.WriteString("#include <stdint.h>\nstruct bitfield { union { uint64_t val; struct {\n")
	for i := 0; i < 64; i++ {
		fmt.Fprintf(&src, "uint64_t u64_bit_%d : 1;\n", i)
	}
	src.WriteString("}; }; };\nstruct bitfield masks[] = {\n")
	for i := 0; i < 64; i++ {
		fmt.Fprintf(&src, "{.u64_bit_%d = 1},\n", i)
	}
	src.WriteString("};\n")
	dir, err := os.MkdirTemp("", "mkwasm")
	if err != nil {
		fail("%v", err)
	}
	defer os.RemoveAll(dir)
	c, o := filepath.Join(dir, "abi.c"), filepath.Join(dir, "abi.o")
	if err := os.WriteFile(c, src.Bytes(), 0o644); err != nil {
		fail("%v", err)
	}
	if out, err := exec.Command(cc, "-c", "-o", o, c).CombinedOutput(); err != nil {
		fail("%s: %v\n%s", cc, err, out)
	}
	f, err := elf.Open(o)
	if err != nil {
		fail("%v", err)
	}
	syms, err := f.Symbols()
	if err != nil {
		fail("%v", err)
	}
	var masks *elf.Symbol
	for i := range syms {
		if syms[i].Name == "masks" {
			masks = &syms[i]
		}
	}
	data, err := f.Section(".data").Data()
	if masks == nil || err != nil {
		fail("no masks in .data: %v", err)
	}
	for i := uint64(0); i < 64; i++ {
		off := masks.Value + i*8
		fmt.Printf("#define BITFIELD_MASK_%d %dULL\n", i, binary.LittleEndian.Uint64(data[off:off+8]))
	}
}

func merge(arch string, commons []string) {
	fset := token.NewFileSet()
	have := map[string]bool{}
	for _, common := range commons {
		cf, err := parser.ParseFile(fset, common, nil, parser.ParseComments)
		if err != nil {
			fail("%v", err)
		}
		declared(cf, have)
	}
	af, err := parser.ParseFile(fset, arch, nil, parser.ParseComments)
	if err != nil {
		fail("%v", err)
	}
	keep(fset, af, func(name string) bool { return !have[name] })
	write(fset, af, arch)
}

// declared adds the names f declares (methods as Type.Name) to have
func declared(f *ast.File, have map[string]bool) {
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				have[d.Name.Name] = true
			} else {
				have[recvName(d)+"."+d.Name.Name] = true
			}
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					have[s.Name.Name] = true
				case *ast.ValueSpec:
					for _, n := range s.Names {
						have[n.Name] = true
					}
				}
			}
		}
	}
}

// keep drops f's declarations whose names ok says not to keep
func keep(fset *token.FileSet, f *ast.File, ok func(string) bool) {
	var decls []ast.Decl
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil {
				name = recvName(d) + "." + name
			}
			if !ok(name) {
				continue
			}
		case *ast.GenDecl:
			var specs []ast.Spec
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					if !ok(s.Name.Name) {
						continue
					}
				case *ast.ValueSpec:
					k := false
					for _, n := range s.Names {
						if ok(n.Name) {
							k = true
						}
					}
					if !k {
						continue
					}
				}
				specs = append(specs, s)
			}
			if len(specs) == 0 && d.Tok != token.IMPORT {
				continue
			}
			d.Specs = specs
		}
		decls = append(decls, d)
	}
	f.Decls = decls
	// imports no longer used
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		var specs []ast.Spec
		for _, sp := range gd.Specs {
			is := sp.(*ast.ImportSpec)
			name := strings.Trim(is.Path.Value, `"`)
			name = name[strings.LastIndex(name, "/")+1:]
			if is.Name != nil {
				name = is.Name.Name
			}
			if used[name] || name == "_" {
				specs = append(specs, sp)
			}
		}
		gd.Specs = specs
	}
	// (comments of what was dropped would be left floating: keep only the
	// file's leading ones, and those in what's kept)
	var cs []*ast.CommentGroup
	for _, c := range f.Comments {
		if c.Pos() < f.Package || within(c, decls) {
			cs = append(cs, c)
		}
	}
	f.Comments = cs
}

func within(c *ast.CommentGroup, decls []ast.Decl) bool {
	for _, d := range decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Doc == c {
			return true
		}
		if c.Pos() >= d.Pos() && c.End() <= d.End() {
			return true
		}
	}
	return false
}

func write(fset *token.FileSet, f *ast.File, path string) {
	var out bytes.Buffer
	if err := format.Node(&out, fset, f); err != nil {
		fail("%v", err)
	}
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		fail("%v", err)
	}
}

// split moves the named functions from common into out, built for linux
// but linux/wasm; idempotent (out is made again with what common has now,
// and what out had)
func split(common, out string, names []string) {
	moving := map[string]bool{}
	for _, n := range names {
		moving[n] = true
	}
	fset := token.NewFileSet()
	src, err := os.ReadFile(common)
	if err != nil {
		fail("%v", err)
	}
	cf, err := parser.ParseFile(fset, common, src, parser.ParseComments)
	if err != nil {
		fail("%v", err)
	}
	var moved bytes.Buffer
	if old, err := os.ReadFile(out); err == nil {
		of, err := parser.ParseFile(fset, out, old, parser.ParseComments)
		if err != nil {
			fail("%v", err)
		}
		for _, d := range of.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				moved.Write(old[fset.Position(fd.Pos()).Offset:fset.Position(fd.End()).Offset])
				moved.WriteString("\n\n")
			}
		}
	}
	for _, d := range cf.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && moving[fd.Name.Name] {
			start := fd.Pos()
			if fd.Doc != nil {
				start = fd.Doc.Pos()
			}
			moved.Write(src[fset.Position(start).Offset:fset.Position(fd.End()).Offset])
			moved.WriteString("\n\n")
		}
	}
	keep(fset, cf, func(name string) bool { return !moving[name] })
	write(fset, cf, common)
	header := "// Code generated by linux/mkwasm.go split from " + common + "; DO NOT EDIT.\n\n" +
		"//go:build linux && !wasm\n\npackage unix\n\nimport (\n\t\"syscall\"\n\t\"unsafe\"\n)\n\n" +
		"var _ syscall.Errno\nvar _ unsafe.Pointer\n\n"
	b, err := format.Source(append([]byte(header), moved.Bytes()...))
	if err != nil {
		fail("%s: %v", out, err)
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		fail("%v", err)
	}
}

func recvName(d *ast.FuncDecl) string {
	t := d.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func leading(f *ast.File) []*ast.CommentGroup {
	var cs []*ast.CommentGroup
	for _, c := range f.Comments {
		if c.Pos() < f.Package {
			cs = append(cs, c)
		}
	}
	return cs
}
