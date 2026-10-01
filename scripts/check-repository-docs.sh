#!/bin/sh
set -eu

cd "$(git rev-parse --show-toplevel)"

check_paths() {
  LC_ALL=C awk -v RS='\0' '
    {
      path = tolower($0)
      name = path
      sub(/^.*\//, "", name)
      if (name ~ /^readme(\.[^/]*)?$/) next
      if (path ~ /\.(md|mdx|markdown|rst|adoc|pdf|docx?|odt|rtf)$/ ||
          path ~ /(^|\/)(docs|documentation|specs|specifications|\.specify|\.spec-kit)\//) {
        print "Local documentation must not be committed: " $0 > "/dev/stderr"
        blocked = 1
      }
    }
    END { exit blocked ? 1 : 0 }
  '
}

case "${1:---index}" in
  --index)
    git ls-files -z | check_paths
    ;;
  --tree)
    revision="${2:?Provide a Git revision}"
    git rev-parse --verify "$revision^{tree}" >/dev/null
    git ls-tree -rz --name-only "$revision" | check_paths
    ;;
  *)
    echo "Usage: $0 [--index | --tree REVISION]" >&2
    exit 2
    ;;
esac
