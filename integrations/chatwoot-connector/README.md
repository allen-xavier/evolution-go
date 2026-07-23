# Conector Chatwoot independente

Este serviço mantém a integração fora do processo e da imagem do Evolution Go.
O Evolution roda diretamente com `evoapicloud/evolution-go:0.7.2`; o conector
consome os eventos globais `MESSAGE` e `SEND_MESSAGE` pelo RabbitMQ e envia as
respostas do Chatwoot pelos endpoints oficiais `/send/text` e `/send/media`.

## Atualização do Evolution

Altere somente a tag abaixo em `docker-stack.swarm.yml`:

```yaml
image: evoapicloud/evolution-go:0.7.2
```

O conector tem imagem, build e ciclo de release próprios. Antes de promover uma
nova versão do Evolution, valide os contratos de evento e dos endpoints de envio
em homologação.

## Build e publicação do conector

O Docker Swarm não faz build durante `docker stack deploy`. Publique a imagem em
um registry acessível pelos nós:

```bash
docker build -t ghcr.io/allen-xavier/evolution-go-chatwoot-connector:0.3.1 .
docker push ghcr.io/allen-xavier/evolution-go-chatwoot-connector:0.3.1
```

## Deploy

Crie apenas o novo volume e carregue as variáveis sem gravar segredos no YAML:

```bash
docker volume create evolution_rabbitmq_data
set -a
. ./.env.swarm
set +a
docker stack deploy -c docker-stack.swarm.yml evolution
```

Se a imagem for entregue como arquivo em vez de registry, carregue-a no nó
manager e impeça a resolução remota durante o deploy:

```bash
docker load -i evolution-go-chatwoot-connector-0.3.1-linux-amd64.tar.gz
docker stack deploy --resolve-image never -c docker-stack.swarm.yml evolution
```

O banco atual pode ser mantido. As tabelas `chatwoot_configs` e
`chatwoot_bindings` continuam sendo usadas pelo conector, mas a imagem oficial do
Evolution não as conhece nem as altera.

## Painel online

Depois do deploy, acesse o painel no mesmo host da Evolution:

```text
https://evogo.example.com/chatwoot
```

Entre com a mesma `GLOBAL_API_KEY` configurada na stack. O painel consulta as
instâncias diretamente na API oficial da Evolution, mostra o estado da conexão,
carrega a configuração existente e salva os dados do Chatwoot no PostgreSQL. A
chave administrativa é mantida apenas na sessão da aba do navegador, e os tokens
individuais das instâncias nunca são enviados ao painel.

## Configuração de uma instância pela API

As rotas foram mantidas compatíveis com a integração anterior e o Traefik envia
somente `/chatwoot/*` ao conector:

```bash
curl -X POST "https://evogo.example.com/chatwoot/set/INSTANCE_UUID" \
  -H "apikey: $GLOBAL_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "url": "https://chatwoot.example.com",
    "accountId": "1",
    "token": "CHATWOOT_ACCESS_TOKEN",
    "inboxId": 1,
    "signMsg": true,
    "signDelimiter": "\\n",
    "mergeBrazilContacts": true,
    "reopenConversation": true,
    "ignoreJids": []
  }'
```

No Chatwoot, configure o webhook da caixa para:

```text
https://evogo.example.com/chatwoot/webhook/INSTANCE_UUID
```

O health check interno do conector está disponível em `/healthz`, mas essa rota
não é publicada pelo router do Traefik fornecido.
