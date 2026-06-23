#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
mode="check"

while [[ $# -gt 0 ]]; do
	case "$1" in
		--write)
			mode="write"
			shift
			;;
		--check)
			mode="check"
			shift
			;;
		*)
			echo "unknown argument: $1" >&2
			exit 1
			;;
	esac
done

go_licenses_module="${GO_LICENSES_MODULE:-github.com/google/go-licenses@v1.6.0}"
packages="${LICENSE_PACKAGES:-./...}"
license_targets="${LICENSE_AUDIT_TARGETS:-${LICENSE_CHECK_TARGETS:-linux/amd64}}"
include_tests="${LICENSE_AUDIT_INCLUDE_TESTS:-${LICENSE_CHECK_INCLUDE_TESTS:-false}}"
new_status="${LICENSE_AUDIT_NEW_STATUS:-needs-review}"
lock_file="${LICENSE_LOCK_FILE:-$repo_root/licenses.lock.yml}"
module_path="$(cd "$repo_root" && go list -m)"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

go_licenses_bin="$tmpdir/bin/go-licenses$(go env GOEXE)"
active_goroot="$(go env GOROOT)"

(cd "$repo_root" && GOBIN="$tmpdir/bin" go install "$go_licenses_module")

ignore_flags=(--ignore "$module_path")
while IFS= read -r module; do
	if [[ -n "$module" ]]; then
		ignore_flags+=(--ignore "$module")
	fi
done < <((cd "$repo_root" && go run ./cmd/licenseaudit --mode ignored-modules --lock "$lock_file"))

license_test_flags=()
case "$include_tests" in
	1 | true | TRUE | yes | YES)
		license_test_flags=(--include_tests)
		;;
	0 | false | FALSE | no | NO | "")
		;;
	*)
		echo "LICENSE_AUDIT_INCLUDE_TESTS must be true or false, got: $include_tests" >&2
		exit 1
		;;
esac

modules_file="$tmpdir/modules.txt"
csv_file="$tmpdir/licenses.csv"
: >"$csv_file"

(cd "$repo_root" && go list -m all >"$modules_file")

for target in $license_targets; do
	goos="${target%/*}"
	goarch="${target#*/}"
	label="$goos-$goarch"
	target_out="$tmpdir/$label.csv"
	target_log="$tmpdir/$label.log"

	if ! (cd "$repo_root" && GOOS="$goos" GOARCH="$goarch" GOROOT="$active_goroot" "$go_licenses_bin" csv "$packages" \
		"${license_test_flags[@]}" \
		"${ignore_flags[@]}" \
		>"$target_out" 2>"$target_log"); then
		{
			printf 'go-licenses csv failed for %s\n' "$target"
			cat "$target_out"
			cat "$target_log"
		} >&2
		exit 1
	fi

	cat "$target_out" >>"$csv_file"
done

(cd "$repo_root" && go run ./cmd/licenseaudit \
	--mode "$mode" \
	--lock "$lock_file" \
	--csv "$csv_file" \
	--modules "$modules_file" \
	--new-status "$new_status" \
	--tool "$go_licenses_module" \
	--packages "$packages" \
	--targets "$license_targets" \
	--include-tests="$include_tests")
