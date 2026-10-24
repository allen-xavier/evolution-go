# Evolution Go (fork allen-xavier)

Fork do [Evolution Go](https://github.com/evolution-foundation/evolution-go) (API de WhatsApp em Go) mantido por allen-xavier.
As mudanças próprias ficam fora do código upstream:
- `integrations/chatwoot-connector/`: conector Chatwoot por instância (serviço separado, imagem própria no GHCR).
- `customizations/patches/`: patches aplicados sobre o Evolution Go a cada release (ver `customizations/README.md`). Não altere o código original do Evolution diretamente; mudanças no core devem virar patch.
- Git: push só para o remoto `allen` (github.com/allen-xavier/evolution-go). Nunca fazer commit/push
  para o `origin` (evolution-foundation, o Evolution original) nem alterar o código original.
- Deploy: imagens em `ghcr.io/allen-xavier/...`, stack Docker Swarm atualizada manualmente pelo usuário no Portainer. O sistema está em produção.

## Histórico compartilhado (Codex + Claude)
Este projeto é trabalhado tanto pelo Codex quanto pelo Claude.
- Antes de começar qualquer tarefa, leia o HISTORICO.md.
- Ao terminar cada tarefa, acrescente uma entrada no FINAL do HISTORICO.md no formato:
  ### AAAA-MM-DD — <Codex|Claude> — <título curto>
  - O que foi feito
  - Decisões tomadas
  - Pendências
- Nunca coloque senhas, tokens ou conteúdo de .env no histórico.
