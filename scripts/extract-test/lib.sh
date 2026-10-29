#!/usr/bin/env bash
#
# Shared helpers for the module extraction tests.
#
# An extraction test copies a package out of the monolith into a clean Go module
# and proves it builds, tests and runs there with no hidden dependency on the rest
# of ForgeCRM. See README.md for what "hidden dependency" means and why a green
# monolith build is not evidence.
#
# This file provides primitives only. Each module owns its own recipe
# (scripts/extract-test/<module>/setup.sh), because what to copy and which
# third-party modules to require differ per module and a universal generator would
# obscure that.

# shellcheck shell=bash

HARNESS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HARNESS_DIR/../.." && pwd)"
BACKEND_DIR="$REPO_ROOT/backend"

# A scratch root for the extracted projects. Overridable so CI can place it on a
# faster disk, and so a diagnostic run can keep the result.
EXTRACT_TEST_ROOT="${EXTRACT_TEST_ROOT:-${TMPDIR:-/tmp}/forgecrm-extract-test}"
# EXTRACT_TEST_KEEP=1 leaves the extracted project in place after the run.
EXTRACT_TEST_KEEP="${EXTRACT_TEST_KEEP:-0}"

# --- Go toolchain -----------------------------------------------------------
# Prefer go on PATH, fall back to the documented /usr/local/go location.
if command -v go >/dev/null 2>&1; then
  GO_BIN="$(command -v go)"
elif [ -x /usr/local/go/bin/go ]; then
  GO_BIN="/usr/local/go/bin/go"
else
  GO_BIN="go"
fi
export PATH="$(dirname "$GO_BIN"):$PATH"
# The toolchain is pinned by the module's `go` directive; do not let Go fetch a
# different one during an extraction test.
export GOTOOLCHAIN=local

# --- Output -----------------------------------------------------------------
step() { printf '\n== %s ==\n' "$*"; }
info() { printf '   %s\n' "$*"; }
ok()   { printf '   ok    %s\n' "$*"; }
warn() { printf '   warn  %s\n' "$*"; }
die()  { printf '   FAIL  %s\n' "$*" >&2; exit 1; }

# --- Primitives -------------------------------------------------------------

# fresh_work <module> prints a clean working directory for the module.
fresh_work() {
  local mod="$1"
  local work="$EXTRACT_TEST_ROOT/$mod"
  rm -rf "$work"
  mkdir -p "$work"
  printf '%s' "$work"
}

# extract_pkg <work> <pkg> <dest-rel> <import-path>
#
# Copies backend/internal/<pkg>/*.go into <work>/<dest-rel> and rewrites every
# reference to github.com/geff1778/forgecrm/internal/<pkg> to <import-path>.
#
# Only the package's own .go files are copied. No other package, no dotfiles, no
# test fixtures from elsewhere in the tree: the copy is the package and nothing
# more, so a hidden dependency shows up as a compile error rather than a silent
# pull-in.
extract_pkg() {
  local work="$1" pkg="$2" dest="$3" import_path="$4"
  local src="$BACKEND_DIR/internal/$pkg"
  [ -d "$src" ] || die "no such package: internal/$pkg"
  local target="$work"
  [ -n "$dest" ] && target="$work/$dest"
  mkdir -p "$target"
  cp "$src"/*.go "$target/" || die "copy internal/$pkg"
  # Rewrite the monolith import path to the local one. A package with no such
  # import is unaffected, so this is safe to call for every package.
  find "$work" -name '*.go' -print0 |
    xargs -0 sed -i "s#github.com/geff1778/forgecrm/internal/$pkg#$import_path#g"
  info "copied internal/$pkg -> ${dest:-.} ($(ls "$target"/*.go 2>/dev/null | wc -l | tr -d ' ') files)"
}

# install_module_files <work> <module-dir> copies the module's go.mod, optional
# go.sum and smoke program into the working directory.
install_module_files() {
  local work="$1" mod_dir="$2"
  cp "$mod_dir/go.mod" "$work/go.mod" || die "copy go.mod"
  if [ -f "$mod_dir/go.sum" ]; then
    cp "$mod_dir/go.sum" "$work/go.sum"
  fi
  mkdir -p "$work/cmd/smoke"
  cp "$mod_dir/cmd/smoke/main.go" "$work/cmd/smoke/main.go" || die "copy smoke program"
}

# rewrite_import <work> <old-import> <new-import> rewrites one import path in
# every .go file under the working directory. It is used to re-point a package at
# another extracted package (objectstore -> netguard) and to point the smoke
# program at the extracted copy.
rewrite_import() {
  local work="$1" old="$2" new="$3"
  find "$work" -name '*.go' -print0 |
    xargs -0 sed -i "s#$old#$new#g"
}

# assert_no_monolith_import <work> fails if any copied file still names ForgeCRM.
assert_no_monolith_import() {
  local work="$1"
  if grep -rn "github.com/geff1778/forgecrm" "$work" --include='*.go' >/dev/null 2>&1; then
    grep -rn "github.com/geff1778/forgecrm" "$work" --include='*.go' >&2
    die "the extracted project still imports the ForgeCRM monolith"
  fi
  ok "no import of the ForgeCRM monolith"
}

# --- Checks -----------------------------------------------------------------

run_build() {
  local work="$1"
  ( cd "$work" && "$GO_BIN" build ./... ) || die "go build ./..."
  ok "go build ./..."
}

run_tests() {
  local work="$1"
  ( cd "$work" && "$GO_BIN" test -count=1 ./... ) || die "go test ./..."
  ok "go test ./..."
}

run_smoke() {
  local work="$1"
  ( cd "$work" && "$GO_BIN" run ./cmd/smoke ) || die "go run ./cmd/smoke"
  ok "go run ./cmd/smoke"
}

# cleanup_work <work> removes the extracted project unless a diagnostic run asked
# to keep it.
cleanup_work() {
  local work="$1"
  if [ "$EXTRACT_TEST_KEEP" = "1" ]; then
    info "kept $work (EXTRACT_TEST_KEEP=1)"
  else
    rm -rf "$work"
  fi
}
