#!/bin/bash
# No prereleases: bump_version splits on `.`, and `sort -V` puts v0.1.0-rc.1 after v0.1.0.

set -euo pipefail

# Must match release.yml's tag filter, or the tag publishes nothing.
VERSION_REGEX="^v?[0-9]+\.[0-9]+\.[0-9]+$"

# grep exits 1 when no tag matches.
get_latest_version() {
    git tag -l "v*" | { grep -E "$VERSION_REGEX" || true; } | sort -V | tail -1 | sed 's/^v//'
}

validate_version() {
    if echo "$1" | grep -qE "$VERSION_REGEX"; then echo "valid"; else echo "invalid"; fi
}

bump_version() {
    local version=$1
    local type=$2

    echo "$version" | awk -F. -v type="$type" '{
        if (type == "major") {
            print $1+1".0.0"
        } else if (type == "minor") {
            print $1"."$2+1".0"
        } else if (type == "patch") {
            print $1"."$2"."$3+1
        } else {
            print "invalid"
        }
    }'
}

determine_version() {
    local version=${1:-}
    local type=${2:-}

    if [ -z "$version" ]; then
        current=$(get_latest_version)
        if [ -z "$current" ]; then
            # First release. The contract has no consumers yet, so it starts
            # below 1.0 and may still break: metacensus/ui#51 adopts it.
            echo "0.1.0"
        else
            if [ -z "$type" ]; then
                echo "Error: TYPE must be specified (major, minor, or patch) when VERSION is not provided" >&2
                exit 1
            fi
            bumped=$(bump_version "$current" "$type")
            if [ "$bumped" = "invalid" ]; then
                echo "Error: Invalid version bump type '$type'. Must be major, minor, or patch" >&2
                exit 1
            fi
            echo "$bumped"
        fi
    else
        version=$(echo "$version" | sed 's/^v//')
        if [ "$(validate_version "$version")" != "valid" ]; then
            echo "Error: Invalid version format '$version'. Must match semver format (e.g., 1.0.0)" >&2
            exit 1
        fi
        echo "$version"
    fi
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    determine_version "$@"
fi
