#!/bin/sh
set -e

git config core.hooksPath .husky
echo "Git hooks installed: core.hooksPath=$(git config core.hooksPath)"
