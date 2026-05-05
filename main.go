// catall recursively prints the contents of files in a directory tree,
// similar to combining find + cat. Designed for quick code review and
// feeding file contents into LLM context windows.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ANSI escape codes; only emitted when stdout is a real terminal.
const (
	ansiCyan  = "\033[1;36m"
	ansiReset = "\033[0m"
)

// config is the resolved runtime configuration derived from CLI flags.
type config struct {
	root         string
	depth        int // -1 = unlimited
	extensions   map[string]bool
	excludeDirs  map[string]bool
	withFilename bool
	maxSizeBytes int64 // 0 = unlimited
	useColor     bool
}

func main() {
	var (
		depthFlag    = flag.Int("depth", -1, "max recursion depth (-1 = unlimited, 0 = root directory only)")
		extFlag      = flag.String("ext", "", `comma-separated extensions to include, e.g. ".go,.md,.txt"`)
		excludeFlag  = flag.String("exclude", "", `comma-separated directory names to skip, e.g. "node_modules,.git"`)
		withFilename = flag.Bool("with-filename", true, "print a header with the file path before each file's contents")
		maxSizeMB    = flag.Float64("max-size", 0, "skip files larger than N megabytes (0 = no limit)")
		noColor      = flag.Bool("no-color", false, "disable ANSI colored headers")
	)

	flag.Usage = printUsage
	flag.Parse()

	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}

	if _, err := os.Stat(root); err != nil {
		fmt.Fprintf(os.Stderr, "catall: %v\n", err)
		os.Exit(1)
	}

	cfg := config{
		root:         root,
		depth:        *depthFlag,
		withFilename: *withFilename,
		// Only emit color when stdout is a real terminal; piping should be clean.
		useColor: !*noColor && isTerminal(os.Stdout),
	}

	if *maxSizeMB > 0 {
		cfg.maxSizeBytes = int64(*maxSizeMB * 1024 * 1024)
	}
	if *extFlag != "" {
		cfg.extensions = parseCSV(*extFlag)
	}
	if *excludeFlag != "" {
		cfg.excludeDirs = parseCSV(*excludeFlag)
	}

	if err := walkDir(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "catall: %v\n", err)
		os.Exit(1)
	}
}

// walkDir traverses the directory tree rooted at cfg.root and prints
// every qualifying file's contents using filepath.WalkDir, which is
// more efficient than filepath.Walk because it avoids extra os.Lstat
// calls on each entry.
func walkDir(cfg config) error {
	absRoot, err := filepath.Abs(cfg.root)
	if err != nil {
		return err
	}

	return filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		// Log inaccessible entries but keep walking the rest of the tree.
		if err != nil {
			fmt.Fprintf(os.Stderr, "catall: warning: %v\n", err)
			return nil
		}

		relPath, relErr := filepath.Rel(absRoot, path)
		if relErr != nil {
			relPath = path
		}

		if d.IsDir() {
			if relPath == "." {
				return nil // always descend into the root itself
			}

			// Skip excluded directories immediately without descending.
			if cfg.excludeDirs[d.Name()] {
				return filepath.SkipDir
			}

			// Depth gate: relPath "sub/deep" has 1 separator → it lives at
			// depth 2 relative to root. We stop descending when the number of
			// separators in the directory's relPath equals cfg.depth, because
			// any children of that directory would exceed the limit.
			if cfg.depth >= 0 {
				if strings.Count(relPath, string(filepath.Separator)) >= cfg.depth {
					return filepath.SkipDir
				}
			}

			return nil
		}

		// Skip symlinks, device nodes, named pipes, etc.
		// Type().IsRegular() is false for all of these.
		if !d.Type().IsRegular() {
			return nil
		}

		// Depth gate for files: a file at "sub/file.txt" has 1 separator,
		// meaning it lives 1 level below root (depth 1).
		if cfg.depth >= 0 {
			if strings.Count(relPath, string(filepath.Separator)) > cfg.depth {
				return nil
			}
		}

		// Extension filter (case-insensitive).
		if len(cfg.extensions) > 0 {
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if !cfg.extensions[ext] {
				return nil
			}
		}

		// Size check requires a stat; DirEntry.Info() reuses the already-read
		// dirent on most platforms, so it is cheap.
		if cfg.maxSizeBytes > 0 {
			info, statErr := d.Info()
			if statErr != nil {
				fmt.Fprintf(os.Stderr, "catall: warning: cannot stat %s: %v\n", relPath, statErr)
				return nil
			}
			if info.Size() > cfg.maxSizeBytes {
				fmt.Fprintf(os.Stderr, "catall: skipping %s: exceeds --max-size limit\n", relPath)
				return nil
			}
		}

		if printErr := printFile(path, relPath, cfg); printErr != nil {
			fmt.Fprintf(os.Stderr, "catall: warning: %s: %v\n", relPath, printErr)
		}

		return nil
	})
}

// printFile streams a single file to stdout, preceded by an optional header.
//
// We read the first 512 bytes once for binary detection, then reassemble the
// full stream with io.MultiReader so we never need to seek or re-open the file.
// A 64 KiB write buffer reduces syscall overhead for large text files.
func printFile(path, relPath string, cfg config) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Probe the first 512 bytes (same window the Unix `file` command uses).
	head := make([]byte, 512)
	n, err := f.Read(head)
	if err != nil && err != io.EOF {
		return err
	}
	head = head[:n]

	if isBinary(head) {
		fmt.Fprintf(os.Stderr, "catall: skipping binary file: %s\n", relPath)
		return nil
	}

	if cfg.withFilename {
		printHeader(relPath, cfg.useColor)
	}

	// Stitch the already-read head back onto the open file descriptor so we
	// stream the complete file without seeking.
	r := io.MultiReader(bytes.NewReader(head), f)
	out := bufio.NewWriterSize(os.Stdout, 64*1024)
	if _, err := io.Copy(out, r); err != nil {
		return err
	}
	if err := out.Flush(); err != nil {
		return err
	}

	// Blank line between files keeps output readable when headers are off.
	fmt.Fprintln(os.Stdout)
	return nil
}

// isBinary returns true when the buffer contains a null byte.
// A null byte is the most reliable cross-platform indicator that a file is
// binary; it is the same heuristic used by git diff and the GNU grep -I flag.
func isBinary(buf []byte) bool {
	return bytes.IndexByte(buf, 0) >= 0
}

// printHeader writes the "===== <path> =====" separator line.
func printHeader(relPath string, useColor bool) {
	const border = "====="
	if useColor {
		fmt.Printf("%s%s %s %s%s\n", ansiCyan, border, relPath, border, ansiReset)
	} else {
		fmt.Printf("%s %s %s\n", border, relPath, border)
	}
}

// parseCSV converts a comma-separated string into a presence set.
func parseCSV(s string) map[string]bool {
	m := make(map[string]bool)
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			m[p] = true
		}
	}
	return m
}

// isTerminal reports whether f is an interactive terminal by checking for the
// character-device mode bit. This avoids any external dependency.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `catall — recursively print file contents to stdout

USAGE:
    catall [flags] [path]

ARGUMENTS:
    path    Root directory to traverse (default: current directory ".")

FLAGS:`)
	flag.PrintDefaults()
	fmt.Fprintln(os.Stderr, `
EXAMPLES:
    # Print all text files in the current directory tree
    catall

    # Traverse ./src up to 2 levels deep
    catall --depth 2 ./src

    # Only include Go and Markdown files
    catall --ext ".go,.md" ./project

    # Skip common noise directories and files larger than 1 MB
    catall --exclude "node_modules,.git,vendor" --max-size 1.0 ./project

    # Pipe without color codes (color is auto-disabled when not a terminal)
    catall --no-color | less

    # Print raw contents only, no filename headers
    catall --with-filename=false ./scripts`)
}
