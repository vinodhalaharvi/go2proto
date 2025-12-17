// go2proto generates Protocol Buffer definitions from Go source code.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vinodhalaharvi/go2proto/pkg/generator"
	"github.com/vinodhalaharvi/go2proto/pkg/parser"
	"github.com/vinodhalaharvi/go2proto/pkg/transformer"
)

var (
	version        = "0.1.0"
	outDir         = flag.String("out", ".", "Output directory for .proto files")
	protoPackage   = flag.String("package", "", "Proto package name (default: derived from Go package)")
	goPackage      = flag.String("go_package", "", "go_package option (default: same as Go import path)")
	includePrivate = flag.Bool("private", false, "Include unexported fields")
	oneFile        = flag.Bool("one-file", false, "Generate a single .proto file for all packages")
	fileName       = flag.String("filename", "", "Output filename (only with -one-file)")
	showVersion    = flag.Bool("version", false, "Show version")
	verbose        = flag.Bool("v", false, "Verbose output")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "go2proto - Generate Protocol Buffer definitions from Go source code\n\n")
		fmt.Fprintf(os.Stderr, "Usage: go2proto [flags] <packages...>\n\n")
		fmt.Fprintf(os.Stderr, "Packages can be:\n")
		fmt.Fprintf(os.Stderr, "  .           Current directory\n")
		fmt.Fprintf(os.Stderr, "  ./...       Current directory and all subdirectories\n")
		fmt.Fprintf(os.Stderr, "  ./models    Specific package\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nComment Tags:\n")
		fmt.Fprintf(os.Stderr, "  // +go2proto=false      Skip this type\n")
		fmt.Fprintf(os.Stderr, "  // +go2proto:service    Generate interface as gRPC service\n")
		fmt.Fprintf(os.Stderr, "  // +go2proto:enum       Generate type alias as enum\n")
	}

	flag.Parse()

	if *showVersion {
		fmt.Printf("go2proto version %s\n", version)
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) == 0 {
		args = []string{"."}
	}

	if err := run(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(patterns []string) error {
	if *verbose {
		fmt.Printf("Parsing packages: %v\n", patterns)
	}

	p := parser.NewParser()
	pkgs, err := p.ParsePackages(patterns...)
	if err != nil {
		return fmt.Errorf("failed to parse packages: %w", err)
	}

	if len(pkgs) == 0 {
		return fmt.Errorf("no packages found matching: %v", patterns)
	}

	if *verbose {
		fmt.Printf("Found %d package(s)\n", len(pkgs))
		for _, pkg := range pkgs {
			fmt.Printf("  - %s (%d structs, %d interfaces)\n", pkg.Path, len(pkg.Structs), len(pkg.Interfaces))
		}
	}

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	opts := transformer.DefaultOptions()
	opts.PackageName = *protoPackage
	opts.GoPackage = *goPackage
	opts.IncludePrivate = *includePrivate

	gen := generator.NewGenerator()
	trans := transformer.NewTransformer(opts)

	if *oneFile {
		return generateSingleFile(pkgs, trans, gen)
	}
	return generatePerPackage(pkgs, trans, gen)
}

func generateSingleFile(pkgs []parser.GoPackage, trans *transformer.Transformer, gen *generator.Generator) error {
	proto := trans.Transform(pkgs)
	content := gen.Generate(proto)

	filename := *fileName
	if filename == "" {
		if proto.Package != "" {
			filename = strings.ReplaceAll(proto.Package, ".", "_") + ".proto"
		} else {
			filename = "generated.proto"
		}
	}

	outPath := filepath.Join(*outDir, filename)
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", outPath, err)
	}

	if *verbose {
		fmt.Printf("Generated: %s\n", outPath)
	} else {
		fmt.Println(outPath)
	}
	return nil
}

func generatePerPackage(pkgs []parser.GoPackage, trans *transformer.Transformer, gen *generator.Generator) error {
	for _, pkg := range pkgs {
		if len(pkg.Structs) == 0 && len(pkg.Interfaces) == 0 {
			if *verbose {
				fmt.Printf("Skipping empty package: %s\n", pkg.Path)
			}
			continue
		}

		proto := trans.Transform([]parser.GoPackage{pkg})
		if len(proto.Messages) == 0 && len(proto.Services) == 0 && len(proto.Enums) == 0 {
			if *verbose {
				fmt.Printf("Skipping package with no proto types: %s\n", pkg.Path)
			}
			continue
		}

		content := gen.Generate(proto)
		filename := pkg.Name + ".proto"
		outPath := filepath.Join(*outDir, filename)

		if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", outPath, err)
		}

		if *verbose {
			fmt.Printf("Generated: %s (%d messages, %d services, %d enums)\n",
				outPath, len(proto.Messages), len(proto.Services), len(proto.Enums))
		} else {
			fmt.Println(outPath)
		}
	}
	return nil
}
