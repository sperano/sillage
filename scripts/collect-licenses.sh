#!/bin/sh

# Copies the license, notice and patent files of everything linked into the
# sillage binary into DEST. The image ships that binary, and the licenses of
# the Go toolchain and of most dependencies (BSD, MIT, Apache-2.0 NOTICE
# files) require these texts to travel with a binary distribution.
#
#   DEST/go/                    Go standard library and runtime
#   DEST/go/vendor/<path>/      files of the modules the standard library
#                               vendors (GOROOT/src/vendor), for each linked
#                               package, as for DEST/modules
#   DEST/modules/<path>@<ver>/  files found in each linked package directory
#                               and its parents up to the module root, at the
#                               same relative path
#   .../source/                 full module source, for MPL-2.0 modules only
#                               (MPL-2.0 section 3.2 asks for the source)
#
# Run it from the module root with the environment of the build (GOOS,
# GOARCH, CGO_ENABLED, GOFLAGS for the build tags). It fails when a linked
# module has no license file, so a new dependency cannot ship without one.
# Adapted from puckdb's scripts/collect-licenses.sh.

set -eu

dest=${1:?usage: collect-licenses.sh DEST}
pkg=./cmd/sillage

# is_license_text FILE succeeds for a file named as a license proper.
is_license_text() {
	name=$(basename "$1" | tr '[:upper:]' '[:lower:]')
	case $name in
	*.go) return 1 ;;
	licen[cs]e* | copying* | unlicense*) return 0 ;;
	esac
	return 1
}

# is_license_file FILE succeeds for a license, notice or patent file.
is_license_file() {
	is_license_text "$1" && return 0
	name=$(basename "$1" | tr '[:upper:]' '[:lower:]')
	case $name in
	*.go) return 1 ;;
	notice* | patents*) return 0 ;;
	esac
	return 1
}

# copy_package_licenses OUT ROOTDIR PKGDIR copies the license files of PKGDIR
# and of each parent directory up to ROOTDIR into OUT, at the same path
# relative to ROOTDIR.
copy_package_licenses() {
	outroot=$1
	rootdir=$2
	dir=$3
	while :; do
		rel=${dir#"$rootdir"}
		for f in "$dir"/*; do
			out="$outroot$rel"
			# Sibling packages share parents: copy each file once.
			if [ -f "$f" ] && [ ! -e "$out/${f##*/}" ] && is_license_file "$f"; then
				mkdir -p "$out"
				cp "$f" "$out/"
			fi
		done
		if [ "$dir" = "$rootdir" ] || [ "$dir" = / ]; then
			return
		fi
		dir=$(dirname "$dir")
	done
}

# has_license_text DIR succeeds when DIR holds a license proper, not only
# NOTICE or PATENTS files.
has_license_text() {
	find "$1" -type f | (
		while read -r f; do
			if is_license_text "$f"; then
				exit 0
			fi
		done
		exit 1
	)
}

# check_module MOD MODDIR fails when MOD has no license file, and copies the
# source of an MPL-2.0 module.
check_module() {
	mod=$1
	moddir=$2
	if [ ! -d "$dest/modules/$mod" ] || ! has_license_text "$dest/modules/$mod"; then
		echo "collect-licenses: no license file in $mod ($moddir)" >&2
		exit 1
	fi
	if grep -rqs 'Mozilla Public License' "$dest/modules/$mod" &&
		[ ! -d "$dest/modules/$mod/source" ]; then
		cp -R "$moddir" "$dest/modules/$mod/source"
	fi
}

mkdir -p "$dest/go"
goroot=$(go env GOROOT)
cp "$goroot/LICENSE" "$goroot/PATENTS" "$dest/go/"

# Captured before filtering: sh has no pipefail to report a go list failure.
listing=$(go list -deps \
	-f '{{with .Module}}{{if not .Main}}{{.Path}}@{{.Version}} {{.Dir}} {{end}}{{end}}{{.Dir}}' "$pkg")

# Standard library and sillage packages print a bare directory. Go's own
# LICENSE covers the standard library, but its vendored modules carry their
# own files: today identical to Go's, which a later release need not keep.
vendor=$goroot/src/vendor
echo "$listing" | grep -v ' ' | sort -u | while read -r pkgdir; do
	case $pkgdir in
	"$vendor"/*) copy_package_licenses "$dest/go/vendor" "$vendor" "$pkgdir" ;;
	esac
done

# Nothing is left while sillage has no dependencies.
packages=$(echo "$listing" | grep ' ' | sort -u)

if [ -n "$packages" ]; then
	echo "$packages" | while read -r mod moddir pkgdir; do
		copy_package_licenses "$dest/modules/$mod" "$moddir" "$pkgdir"
	done

	echo "$packages" | while read -r mod moddir _; do
		echo "$mod $moddir"
	done | sort -u | while read -r mod moddir; do
		check_module "$mod" "$moddir"
	done
fi

# The module cache is read-only; let the caller delete or overwrite DEST.
chmod -R u+w "$dest"
