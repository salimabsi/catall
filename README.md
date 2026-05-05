# catall

Recursively print the contents of files in a directory — like `find + cat` in one command.

## Install

```bash
go install github.com/salimabsi/catall@latest
```

Or build from source:

```bash
git clone https://github.com/salimabsi/catall
cd catall
go install .
```

## Usage

```
catall [flags] [path]
```

`path` defaults to the current directory.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--depth N` | `-1` (unlimited) | Max recursion depth. `0` = root dir only |
| `--ext ".go,.md"` | all | Only include files with these extensions |
| `--exclude "a,b"` | none | Add extra directory names to skip |
| `--all` | `false` | Include default-excluded directories (see below) |
| `--max-size N` | `0` (unlimited) | Skip files larger than N megabytes |
| `--with-filename` | `true` | Print a `===== path =====` header before each file |
| `--no-color` | `false` | Disable colored headers (auto-disabled when piping) |

## Default ignored directories

These are skipped automatically. Use `--all` to include them, or `--exclude` to add more on top.

| Category | Directories |
|----------|-------------|
| Version control | `.git` `.svn` `.hg` `.bzr` |
| JS / TS | `node_modules` `.npm` `.yarn` `.pnp` `.pnpm-store` |
| Python | `__pycache__` `.venv` `venv` `env` `.env` `.pytest_cache` `.mypy_cache` `.ruff_cache` `.tox` |
| Go | `vendor` |
| Rust | `target` |
| Java / Kotlin | `.gradle` `.mvn` |
| Build & dist | `build` `dist` `out` `bin` `.build` |
| IDEs & editors | `.idea` `.vscode` `.vs` `.fleet` |
| Frontend tooling | `.next` `.nuxt` `.svelte-kit` `.turbo` `.parcel-cache` |
| Infrastructure | `.terraform` `.terragrunt-cache` |
| Caches & temp | `.cache` `tmp` `temp` `.tmp` |
| Test coverage | `coverage` `htmlcov` `.nyc_output` |

## Examples

```bash
# Print all text files in the current tree (noisy dirs auto-skipped)
catall

# Only Go and Markdown files, 2 levels deep
catall --ext ".go,.md" --depth 2 ./src

# Add extra directories to the ignore list
catall --exclude ".env.local,secrets" ./project

# Bypass the default ignore list entirely
catall --all ./project

# Cap file size at 1 MB and pipe to less
catall --max-size 1.0 --no-color | less

# Feed a whole codebase into an LLM
catall --ext ".go" . | pbcopy
```

## Behavior

- **Default excludes** — common noise directories are skipped out of the box; use `--all` to disable.
- **Binary files** — detected via null-byte probe (first 512 bytes) and skipped automatically.
- **Symlinks** and special files (devices, pipes) are skipped.
- **Permission errors** are logged to stderr; the walk continues.
- **Color** is emitted only when stdout is a terminal — piping is always clean.
