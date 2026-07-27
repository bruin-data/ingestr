#!/usr/bin/env bash

set -euo pipefail

merge_base_ref="${LINT_MERGE_BASE:-origin/main}"
concurrency="${LINT_CONCURRENCY:-4}"
timeout="${LINT_TIMEOUT:-10m}"
parallel_flags="${LINT_PARALLEL_FLAGS:---allow-parallel-runners}"
enable_only="${LINT_ENABLE_ONLY:-}"

merge_base="$(git merge-base "$merge_base_ref" HEAD)"

changed_files=()
while IFS= read -r file; do
  changed_files+=("$file")
done < <(
  {
    git diff --name-only --diff-filter=ACMRD "$merge_base" --
    git ls-files --others --exclude-standard
  } | sed '/^$/d' | LC_ALL=C sort -u
)

if [[ ${#changed_files[@]} -eq 0 ]]; then
  echo "No changes found."
  exit 0
fi

lint_all=false
packages=()

for file in "${changed_files[@]}"; do
  case "$file" in
    go.mod|go.sum|.golangci.yml)
      lint_all=true
      ;;
    *.go)
      if [[ ! -e "$file" ]]; then
        lint_all=true
        continue
      fi

      directory="$(dirname "$file")"
      if [[ "$directory" == "." ]]; then
        packages+=("./")
      else
        packages+=("./$directory")
      fi
      ;;
  esac
done

if [[ "$lint_all" == true ]]; then
  packages=("./...")
elif [[ ${#packages[@]} -eq 0 ]]; then
  echo "No changed Go packages to lint."
  exit 0
else
  unique_packages=()
  while IFS= read -r package; do
    unique_packages+=("$package")
  done < <(printf '%s\n' "${packages[@]}" | LC_ALL=C sort -u)
  packages=("${unique_packages[@]}")
fi

args=(run --timeout "$timeout" --concurrency "$concurrency")

if [[ -n "$parallel_flags" ]]; then
  read -r -a parsed_parallel_flags <<< "$parallel_flags"
  args+=("${parsed_parallel_flags[@]}")
fi

if [[ -n "$enable_only" ]]; then
  args+=(--enable-only "$enable_only")
fi

echo "Packages: ${packages[*]}"
exec golangci-lint "${args[@]}" "${packages[@]}"
