#!/usr/bin/env bash
# Liste des plugins asdf disponibles dans le runner.
#
# Joué au build de l'image uniquement. Les versions réellement pré-installées
# sont listées à part, dans `runner/tool-versions`.

set -euo pipefail

asdf plugin add 'nodejs'
asdf plugin add 'deno'
asdf plugin add 'yq'
asdf plugin add 'golang'
asdf plugin add 'terraform' 'https://github.com/asdf-community/asdf-hashicorp.git'
