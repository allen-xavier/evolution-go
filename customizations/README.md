# Personalizações seguras da Evolution

O código principal da Evolution permanece igual ao upstream. As alterações
locais ficam em `patches/` e devem ser reaplicadas e testadas para cada release.

## Validar uma release sem deixar o patch aplicado

No PowerShell, a partir da raiz do repositório:

```powershell
.\customizations\Test-EvolutionPatches.ps1
```

O script verifica a compatibilidade, aplica os patches temporariamente, procura
caminhos conhecidos de fallback direto, executa os testes, gera a imagem Docker
local e remove os patches temporários mesmo quando um teste falha.

## Aplicar para gerar uma imagem definitiva

```powershell
.\customizations\Apply-EvolutionPatches.ps1 -CheckOnly
.\customizations\Apply-EvolutionPatches.ps1
docker build -t ghcr.io/allen-xavier/evolution-go:SEU-TAG-SEGURO .
```

Ative obrigatoriamente esta variável no serviço da Evolution:

```text
PROXY_REQUIRED=true
```

Sem ela, instâncias que nunca tiveram proxy ainda podem conectar diretamente.
Com ela, ausência, configuração inválida ou falha de autenticação do proxy deixa
a instância desconectada.

O patch também fecha o container de banco ao terminar uma tentativa de conexão,
evitando que falhas repetidas esgotem as conexões do PostgreSQL.

O segundo patch adiciona uma única rotina de reconexão por instância, com espera
de 15 segundos até o limite de 5 minutos. Eventos `LoggedOut`/`device_removed`
interrompem as tentativas automáticas. O monitor do conector apenas alerta por
padrão e não pausa sessões ativas. A pausa terminal até a validação de um proxy
estável e exclusivo só é usada quando o modo opcional
`PROXY_COLLISION_ACTION=quarantine` está habilitado.

O terceiro patch melhora o diagnóstico de webhooks externos. Respostas HTTP não
2xx passam a registrar o evento, status, tamanho do payload e até 2 KiB do corpo
de resposta, e cada tentativa tem timeout de 30 segundos. Isso permite enxergar
o motivo exato de respostas como `400 Bad Request` devolvidas pelo n8n.

O quarto patch adiciona a tabela `evolution_amqp_outbox`. Todo evento destinado
ao RabbitMQ é gravado no PostgreSQL antes de ser aceito pelo Evolution e só é
removido depois da confirmação do broker. Falhas mantêm o evento pendente com
backoff de 5 segundos a 5 minutos, inclusive após reinício do container. Com
`AMQP_GLOBAL_ENABLED=true`, a inicialização das sessões do WhatsApp também fica
pausada até o RabbitMQ estar realmente disponível.

O quinto patch corrige clientes desconectados que permanecem presos na memória.
Os endpoints de conectar, reconectar e gerar QR reiniciam somente a instância
afetada, removem QR antigo e preservam as demais sessões. O pareamento deixa de
retornar sucesso falso quando o WebSocket não está conectado.

O sexto patch torna `GET /instance/status` estritamente somente leitura. A
consulta não cria cliente, não abre WebSocket e não reconecta uma instância.
`Connected` continua representando o transporte WebSocket, enquanto `LoggedIn`
continua representando a autenticação da sessão do WhatsApp.

O sétimo patch isola cada geração do cliente e serializa o ciclo de vida por
instância. Uma reconexão aguarda a geração anterior terminar, só retorna sucesso
quando o WebSocket e o handler de eventos estão ativos e impede que uma goroutine
antiga apague a sessão nova. Falhas consecutivas de keepalive e `StreamReplaced`
passam a recuperar somente a instância afetada.

O décimo patch impede que a reconexão automática fique gerando QR code para
uma instância deslogada. Antes de cada tentativa, o agendador confere se as
credenciais do aparelho ainda existem em `whatsmeow_device`. Sem credenciais
(número deslogado ou bloqueado pelo WhatsApp), ele para, grava o motivo
`Logged out: waiting for a new QR code scan` e aguarda a leitura de um QR novo
pelo manager. Nada é removido: a instância e as configurações de Chatwoot,
proxy e n8n continuam. Instâncias com credenciais seguem reconectando como
antes (quedas de rede e proxy).

O décimo primeiro patch tenta de novo o download da mídia recebida (imagem,
áudio, documento e vídeo) quando ele falha por erro de rede, por exemplo um
proxy que por alguns segundos não alcança o servidor de mídia do WhatsApp
(`NO_HOST_CONNECTION`). Depois das tentativas do próprio whatsmeow (cerca de
15 segundos), espera 5 segundos e tenta de novo; se falhar, espera 15 segundos
e tenta uma última vez. Antes, a mensagem seguia sem o arquivo e chegava ao
Chatwoot só com o nome do documento. Mídia expirada ou corrompida (403/404/410,
hmac inválido) não é repetida, e figurinhas também não. Enquanto espera, as
próximas mensagens da mesma instância aguardam (no pior caso cerca de 1 minuto
e meio a mais, só quando a rede falha).

O décimo segundo patch é o único que mexe na biblioteca `whatsmeow-lib` (submódulo):
traz da versão oficial do whatsmeow o tratamento dos nós `<status>` (commit
`a238105`, julho de 2026). O WhatsApp passou a mandar algumas atualizações de
status nesse formato; sem tratamento, a evolution-go nunca confirmava o
recebimento, o servidor derrubava a conexão com
`<stream:error><ack class="status" type="media"/>` e reenviava o mesmo item a cada
reconexão. A fila de mensagens guardadas da instância nunca andava, e a instância
continuava "conectada" e enviando, mas sem receber mensagem, confirmação ou
presença. Agora o nó é confirmado e a fila anda.

O décimo terceiro patch traz do whatsmeow oficial o commit `a7ea563` ("ensure
all nodes get acked"). Mensagem que falha na leitura, decriptação que dá pânico
e notificação com atributo inválido também passam a ser confirmadas. Assim
nenhum outro tipo de nó trava a fila do mesmo jeito.

Os patches 014 a 018 também trazem correções do whatsmeow oficial para a
`whatsmeow-lib` (cherry-pick sobre os patches 012/013, com os ajustes anotados):

- 014: `f331563` (frame grande dividido era remontado com 3 bytes a menos e
  derrubava a conexão), `bb54a07` (nó com tipo estranho causava panic no
  decoder) e `415df31` (panic no tratamento de retry receipt derrubava o
  processo inteiro, com todas as instâncias).
- 015: `f376da2`. Um pkmsg sem estado sobrescrevia a sessão Signal e as
  mensagens seguintes do contato chegavam "Undecryptable".
- 016: `098bcf7`, `563bcaa` e `19c6103` (mapa LID ↔ telefone: mantém o
  aparelho, apaga entradas velhas quando o LID ou o número muda e prefere
  `participant_pn` como telefone).
- 017: `d24d079`. O handshake para de esperar quando o socket fecha. Adaptado:
  aqui `doHandshake` devolve só `error`.
- 018: `bbe00cd`. O ack do retry receipt sai depois do reenvio. Adaptado: sem o
  semáforo opcional de `a5594bf` e sem o wrapper de `handleUnencryptedMessage`
  em `internals.go` (aqui o handler do 012 se chama `handleStatusNode`).

Ficaram para a migração completa da biblioteca (precisam de mais adaptação):
`4fa3462` e `28bfe53` (fila de nós por conexão), que conflitam com o ciclo de
vida dos patches 002/007.

As stacks separadas e o procedimento de migração estão em
`integrations/chatwoot-connector/docker-stack.infrastructure.swarm.yml`,
`docker-stack.application.swarm.yml` e `STACK-MIGRATION.md`. PostgreSQL e
RabbitMQ ficam fora das atualizações rotineiras da aplicação e suas imagens são
fixadas por digest.

## Atualização futura

Depois de colocar uma release nova no repositório, execute primeiro o teste. Se
`git apply --check` falhar, não gere nem publique a imagem: o patch precisa ser
revisado para o novo código.

O bloqueio de rede continua sendo necessário como segunda barreira. Consulte
`firewall-egress.md`.
