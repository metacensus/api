#!/bin/bash
#
# Version management, ported from metacensus/infra. `make release` calls this so
# tags are minted rather than typed: a version that is not semver, or a bump
# type that is not major/minor/patch, fails here instead of becoming a tag.
#
# The release workflow's tag filter is deliberately narrower than `v*` and
# matches VERSION_REGEX below. Keep the two in step.
#
# Prereleases are deliberately NOT supported. bump_version splits on `.`, so
# `0.2.0-rc.1` would come apart as 0 / 2 / 0-rc / 1 and a patch bump would
# coerce `0-rc` to 0 and yield 0.2.1, skipping 0.2.0 entirely; and `sort -V`
# orders v0.1.0-rc.1 after v0.1.0, so get_latest_version would treat an rc as
# the current version. Supporting them properly is more code than this repo
# needs while nothing consumes it. To add them back, widen VERSION_REGEX, add
# the `v[0-9]+.[0-9]+.[0-9]+-*` glob to release.yml, and make both of those
# behaviours correct first — the npm dist-tag guard in release.yml is already
# written to cope.

set -euo pipefail

VERSION_REGEX="^v?[0-9]+\.[0-9]+\.[0-9]+$"

# Get latest version
# `|| true` on the grep: with no tags yet it matches nothing and exits 1,
# which under `set -o pipefail` would abort the whole script instead of
# reporting "no previous version".
get_latest_version() {
    git tag -l "v*" | { grep -E "$VERSION_REGEX" || true; } | sort -V | tail -1 | sed 's/^v//'
}

# Validate version format
validate_version() {
    if echo "$1" | grep -qE "$VERSION_REGEX"; then echo "valid"; else echo "invalid"; fi
}

# Bump version based on type
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

# Main function to determine version
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

# If script is called directly
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    determine_version "$@"
fi
