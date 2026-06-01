#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_file="$repo_root/THIRD_PARTY_LICENSES.txt"
check_only=0

if [[ "${1:-}" == "--check" ]]; then
	check_only=1
	shift
fi

if [[ $# -gt 0 ]]; then
	output_file="$1"
fi

go_licenses_module="${GO_LICENSES_MODULE:-github.com/google/go-licenses@v1.6.0}"
disallowed_license_types="${LICENSE_DISALLOWED_TYPES:-forbidden,restricted,unknown}"
packages="${LICENSE_PACKAGES:-./...}"
license_targets="${LICENSE_TARGETS:-linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64}"
module_path="$(cd "$repo_root" && go list -m)"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

go_licenses_bin="$tmpdir/bin/go-licenses$(go env GOEXE)"

(cd "$repo_root" && GOBIN="$tmpdir/bin" go install "$go_licenses_module")

sha256_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

copy_notice_file() {
	local rel="$1"
	local abs="$2"
	local out="$3"

	{
		printf '\n'
		printf '===============================================================================\n'
		printf '%s\n' "$rel"
		printf '===============================================================================\n\n'
		cat "$abs"
		printf '\n'
	} >>"$out"
}

append_manual_module_license() {
	local module="$1"
	local expected_version="$2"
	local license_file="$3"
	local license_name="$4"
	local expected_sha256="$5"
	local out="$6"

	local module_info version dir selected_sha256

	if ! module_info="$(cd "$repo_root" && go list -m -f '{{.Version}}	{{.Dir}}' "$module" 2>/dev/null)"; then
		return 0
	fi

	version="${module_info%%	*}"
	dir="${module_info#*	}"

	if [[ "$version" != "$expected_version" ]]; then
		cat >&2 <<EOF
Manual license audit for $module is pinned to $expected_version, but go.mod selects $version.
Review $module's license, then update hack/update-third-party-licenses.sh and regenerate THIRD_PARTY_LICENSES.txt.
EOF
		return 1
	fi

	if [[ ! -f "$dir/$license_file" ]]; then
		cat >&2 <<EOF
Manual license audit for $module expected $license_file, but it was not found in $dir.
Review $module's license, then update hack/update-third-party-licenses.sh and regenerate THIRD_PARTY_LICENSES.txt.
EOF
		return 1
	fi

	selected_sha256="$(sha256_file "$dir/$license_file")"
	if [[ "$selected_sha256" != "$expected_sha256" ]]; then
		cat >&2 <<EOF
Manual license audit for $module expected SHA-256 $expected_sha256, but found $selected_sha256.
Review $module's license, then update hack/update-third-party-licenses.sh and regenerate THIRD_PARTY_LICENSES.txt.
EOF
		return 1
	fi

	{
		printf '\n'
		printf '===============================================================================\n'
		printf '%s/%s (%s, manually audited)\n' "$module" "$license_file" "$license_name"
		printf '===============================================================================\n\n'
		cat "$dir/$license_file"
	} >>"$out"
}

save_root="$tmpdir/go-licenses"
generated_file="$tmpdir/THIRD_PARTY_LICENSES.txt"

ignore_flags=(
	--ignore "$module_path"
	# go-licenses v1.6.0 cannot classify these current license files. Keep them
	# pinned below so version or license text changes force a manual review.
	--ignore "github.com/segmentio/asm"
	--ignore "modernc.org/mathutil"
	# This Darwin/cgo package is selected differently by host settings. Keep the
	# required notice deterministic across local machines and CI.
	--ignore "github.com/99designs/go-keychain"
)

run_target() {
	local target="$1"
	local goos="${target%/*}"
	local goarch="${target#*/}"
	local label="$goos-$goarch"
	local target_save_dir="$save_root/$label"
	local target_out="$tmpdir/$label.out"
	local target_log="$tmpdir/$label.log"
	local target_failure="$tmpdir/$label.failed"

	if ! (cd "$repo_root" && GOOS="$goos" GOARCH="$goarch" "$go_licenses_bin" check "$packages" \
		--include_tests \
		"${ignore_flags[@]}" \
		--disallowed_types="$disallowed_license_types" \
		>"$target_out" 2>"$target_log"); then
		{
			printf 'go-licenses check failed for %s\n' "$target"
			cat "$target_out"
			cat "$target_log"
		} >"$target_failure"
		return 1
	fi

	if ! (cd "$repo_root" && GOOS="$goos" GOARCH="$goarch" "$go_licenses_bin" save "$packages" \
		--include_tests \
		"${ignore_flags[@]}" \
		--save_path "$target_save_dir" \
		>"$target_out" 2>"$target_log"); then
		{
			printf 'go-licenses save failed for %s\n' "$target"
			cat "$target_out"
			cat "$target_log"
		} >"$target_failure"
		return 1
	fi
}

pids=()
for target in $license_targets; do
	run_target "$target" &
	pids+=("$!")
done

failed=0
for pid in "${pids[@]}"; do
	if ! wait "$pid"; then
		failed=1
	fi
done

if [[ "$failed" -ne 0 ]]; then
	while IFS= read -r failure; do
		cat "$failure" >&2
	done < <(find "$tmpdir" -maxdepth 1 -type f -name '*.failed' | LC_ALL=C sort)
	exit 1
fi

{
	printf 'Third-Party Licenses\n'
	printf '\n'
	printf 'This file contains third-party Go dependency license notices for ingestr.\n'
	printf 'It is generated by hack/update-third-party-licenses.sh; do not edit it manually.\n'
	printf '\n'
	printf 'Generator: %s\n' "$go_licenses_module"
	printf 'Packages: %s\n' "$packages"
	printf 'License targets: %s\n' "$license_targets"
	printf 'Disallowed license types: %s\n' "$disallowed_license_types"
	printf '\n'
} >"$generated_file"

notice_entries="$tmpdir/notice-entries.tsv"
: >"$notice_entries"

while IFS= read -r target_dir; do
	while IFS= read -r file; do
		rel="${file#"$target_dir"/}"
		printf '%s\t%s\n' "$rel" "$file" >>"$notice_entries"
	done < <(
		find "$target_dir" -type f \
			\( \
				-iname 'LICENSE*' -o \
				-iname 'LICENCE*' -o \
				-iname 'COPYING*' -o \
				-iname 'NOTICE*' -o \
				-iname 'AUTHORS' -o \
				-iname 'PATENTS' \
			\) \
			| LC_ALL=C sort
	)

	while IFS= read -r dir; do
		if ! find "$dir" -maxdepth 1 -type f \
			\( \
				-iname 'LICENSE*' -o \
				-iname 'LICENCE*' -o \
				-iname 'COPYING*' -o \
				-iname 'NOTICE*' \
			\) | grep -q .; then
			while IFS= read -r readme; do
				rel="${readme#"$target_dir"/}"
				printf '%s\t%s\n' "$rel" "$readme" >>"$notice_entries"
			done < <(find "$dir" -maxdepth 1 -type f -iname 'README*' | LC_ALL=C sort)
		fi
	done < <(find "$target_dir" -type d | LC_ALL=C sort)
done < <(find "$save_root" -mindepth 1 -maxdepth 1 -type d | LC_ALL=C sort)

LC_ALL=C sort "$notice_entries" | awk -F '\t' '!seen[$1]++' | while IFS=$'\t' read -r rel file; do
	# go-licenses can emit both the historical GitHub casing and the canonical
	# module casing for go-mssqldb on some hosts. Keep one copy.
	if [[ "$rel" == github.com/Microsoft/go-mssqldb/* ]]; then
		continue
	fi

	copy_notice_file "$rel" "$file" "$generated_file"
done

append_manual_module_license \
	"github.com/segmentio/asm" \
	"v1.2.1" \
	"LICENSE" \
	"MIT No Attribution" \
	"cca993712df289a5958bdef69031a5dac0f951ac15afeb313f9eeea55ed59443" \
	"$generated_file"

append_manual_module_license \
	"modernc.org/mathutil" \
	"v1.7.1" \
	"LICENSE" \
	"BSD-3-Clause" \
	"bfa9bf72a72ca009fd62a8f84fca3dca67e51d93af96352723646599898b6cf5" \
	"$generated_file"

append_manual_module_license \
	"github.com/99designs/go-keychain" \
	"v0.0.0-20191008050251-8e49817e8af4" \
	"LICENSE" \
	"MIT" \
	"039c69774070226d213bced933176be4ec396c9b101cd9a13e82a2c390c6c90a" \
	"$generated_file"

if [[ "$check_only" -eq 1 ]]; then
	if ! cmp -s "$generated_file" "$output_file"; then
		cat >&2 <<EOF
$output_file is stale.
Run 'make licenses' and commit the updated THIRD_PARTY_LICENSES.txt.
EOF
		diff -u "$output_file" "$generated_file" >&2 || true
		exit 1
	fi
else
	cp "$generated_file" "$output_file"
fi
