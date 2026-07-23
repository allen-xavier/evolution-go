# Conector Chatwoot independente

Este serviço mantém a integração fora do processo e da imagem do Evolution Go.
O Evolution roda com uma imagem versionada que contém o patch de proxy
`fail-closed`; o conector
consome os eventos globais `MESSAGE` e `SEND_MESSAGE` pelo RabbitMQ e envia as
respostas do Chatwoot pelos endpoints oficiais `/send/text` e `/send/media`.

## Atualização do Evolution

Na raiz do repositório, valide o patch contra a nova release antes de alterar
`EVOLUTION_IMAGE`:

```powershell
.\customizations\Test-EvolutionPatches.ps1
```

Não use a imagem oficial sem o patch nesta stack: `PROXY_REQUIRED=true` só é
reconhecido pela versão corrigida. O conector tem imagem, build e ciclo de
release próprios.

## Build e publicação do conector

O Docker Swarm não faz build durante `docker stack deploy`. Publique a imagem em
um registry acessível pelos nós:

```bash
docker build -t ghcr.io/allen-xavier/evolution-go-chatwoot-connector:0.5.3 .
docker push ghcr.io/allen-xavier/evolution-go-chatwoot-connector:0.5.3
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
docker load -i evolution-go-chatwoot-connector-0.5.3-linux-amd64.tar.gz
docker stack deploy --resolve-image never -c docker-stack.swarm.yml evolution
```

O banco atual pode ser mantido. Além de `chatwoot_configs` e
`chatwoot_bindings`, o conector cria automaticamente:

- `chatwoot_identity_aliases`, que associa o LID provisório ao número real;
- `chatwoot_outbound_jobs`, que mantém mensagens do Chatwoot aguardando nova
  tentativa de envio ao WhatsApp;
- `chatwoot_proxy_tests`, que registra o último IP validado de cada instância.

Eventos recebidos do Evolution só recebem `ACK` depois de serem aceitos pelo
Chatwoot. Uma falha passa por até oito tentativas persistentes com intervalo de
15 segundos; depois o evento fica disponível nas filas RabbitMQ
`message.chatwoot-dead` ou `sendmessage.chatwoot-dead`.

Quando o envio Chatwoot → WhatsApp falha, o webhook é salvo no PostgreSQL e o
conector tenta novamente com backoff de 15 segundos até 5 minutos. O ID
determinístico da mensagem é mantido em todas as tentativas.

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

Cada instância também possui uma aba **Proxy**. Ela lê a configuração individual
já armazenada na tabela `instances`, mas nunca devolve a senha ao navegador. É
possível:

- alterar host, porta, protocolo, usuário e senha;
- manter a senha atual deixando o campo de senha vazio;
- testar as credenciais sem salvar ou reiniciar a instância;
- confirmar o IP público de saída e o acesso a `web.whatsapp.com`;
- detectar quando duas instâncias apresentam o mesmo IP;
- rejeitar proxies que retornem IPs diferentes em conexões independentes;
- salvar somente depois de um teste aprovado e com IP exclusivo.

No modo obrigatório, a remoção do proxy fica desabilitada. Salvar provoca a
reconexão da instância. O teste não possui fallback para conexão direta: se o
proxy não funcionar, ele retorna erro.

O monitor repete a validação a cada 60 segundos. Por padrão,
`PROXY_COLLISION_ACTION=alert`: IP instável, proxy indisponível ou IP
compartilhado geram um alerta persistente no painel, mas não encerram uma sessão
ativa. Quando a anomalia desaparece, o estado volta automaticamente para
validado; como a sessão não caiu, não é necessário reconectá-la.

Novos proxies continuam sendo aceitos somente quando duas conexões independentes
retornam o mesmo IP e esse IP não aparece em outra instância. Esse controle
exige sessão sticky. Um proxy verdadeiramente rotativo será considerado
inseguro, mas o monitor não ficará derrubando e reconectando uma sessão ativa
para procurar outro IP.

Se a operação preferir isolamento estrito em vez de continuidade, poderá definir
`PROXY_COLLISION_ACTION=quarantine`. Nesse modo opcional, duas observações
consecutivas colocam a instância afetada em quarentena; ela é reconectada quando
o proxy volta a apresentar um IP estável e exclusivo.

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
