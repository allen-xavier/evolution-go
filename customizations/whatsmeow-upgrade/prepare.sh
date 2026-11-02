#!/bin/sh
# Prepara uma CÓPIA da árvore do Evolution Go com o whatsmeow oficial
# (tulir/whatsmeow) no lugar do fork EvolutionAPI, para gerar uma imagem de teste.
#
# Uso:
#   sh customizations/whatsmeow-upgrade/prepare.sh /caminho/da/copia
#
# O diretório informado deve conter a árvore ORIGINAL do Evolution (sem os
# patches aplicados), por exemplo exportada com:
#   git archive HEAD | tar -x -C /caminho/da/copia
#
# Passos:
#   1. troca whatsmeow-lib por um clone do tulir/whatsmeow no sha de UPSTREAM.txt;
#   2. aplica customizations/patches/001..011 (012..018 já estão no upstream);
#   3. aplica customizations/whatsmeow-upgrade/1*.patch.
#
# Nunca rode no repositório de trabalho: o script apaga whatsmeow-lib do destino.

set -eu

UPSTREAM_URL="https://github.com/tulir/whatsmeow.git"

# Pasta deste script (customizations/whatsmeow-upgrade) e a dos patches 001..018.
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
PATCH_DIR="$SCRIPT_DIR/../patches"
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd -P)

if [ $# -ne 1 ]; then
	echo "uso: $0 <diretório com a cópia da árvore do Evolution>" >&2
	exit 2
fi

TARGET=$(CDPATH= cd -- "$1" && pwd -P)

# Proteções: precisa ser a árvore do Evolution e não pode ser o próprio repositório.
if ! grep -q '^module github.com/EvolutionAPI/evolution-go' "$TARGET/go.mod" 2>/dev/null; then
	echo "erro: $TARGET não parece a árvore do Evolution Go (go.mod)" >&2
	exit 1
fi
if [ "$TARGET" = "$REPO_ROOT" ]; then
	echo "erro: use uma cópia da árvore, não o repositório ($REPO_ROOT)" >&2
	exit 1
fi

# Sha do whatsmeow oficial: primeira linha de UPSTREAM.txt com 40 caracteres hex.
SHA=$(grep -E '^[0-9a-f]{40}$' "$SCRIPT_DIR/UPSTREAM.txt" | head -n 1)
if [ -z "$SHA" ]; then
	echo "erro: sha não encontrado em $SCRIPT_DIR/UPSTREAM.txt" >&2
	exit 1
fi

echo "==> whatsmeow-lib = tulir/whatsmeow $SHA"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

# Busca só o commit pedido (o GitHub permite fetch por sha).
git -C "$TMP" init -q
git -C "$TMP" remote add origin "$UPSTREAM_URL"
git -C "$TMP" fetch -q --depth 1 origin "$SHA"
git -C "$TMP" checkout -q FETCH_HEAD
GOT=$(git -C "$TMP" rev-parse HEAD)
if [ "$GOT" != "$SHA" ]; then
	echo "erro: commit baixado ($GOT) diferente do esperado ($SHA)" >&2
	exit 1
fi

# Substitui a biblioteca, sem o .git do clone.
rm -rf "$TARGET/whatsmeow-lib"
mkdir -p "$TARGET/whatsmeow-lib"
git -C "$TMP" archive --format=tar HEAD | tar -x -C "$TARGET/whatsmeow-lib"

# git apply sempre relativo à raiz do destino; GIT_CEILING_DIRECTORIES impede
# que um repositório acima do destino seja usado por engano.
apply_patch() {
	(
		cd "$TARGET"
		GIT_CEILING_DIRECTORIES=$(dirname "$TARGET")
		export GIT_CEILING_DIRECTORIES
		if ! git apply --check --whitespace=nowarn "$1"; then
			echo "erro: $(basename "$1") não aplica nesta árvore" >&2
			exit 1
		fi
		git apply --whitespace=nowarn "$1"
	)
	echo "    ok $(basename "$1")"
}

echo "==> patches 001..011"
for p in "$PATCH_DIR"/0*.patch; do
	name=$(basename "$p")
	case "$name" in
	00[1-9]-* | 010-* | 011-*) apply_patch "$p" ;;
	*) echo "    pulado $name (já está no whatsmeow oficial)" ;;
	esac
done

echo "==> patches da migração (whatsmeow-upgrade/1*.patch)"
for p in "$SCRIPT_DIR"/1*.patch; do
	apply_patch "$p"
done

echo "==> pronto: $TARGET"
echo "    docker build --platform linux/amd64 -t <imagem:tag> \"$TARGET\""
