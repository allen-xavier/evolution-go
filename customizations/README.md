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
