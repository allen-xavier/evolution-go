# Migração para o whatsmeow oficial (TESTE)

Troca a biblioteca `whatsmeow-lib` (fork EvolutionAPI, parado em maio de 2026,
base tulir `02ec817`) pelo whatsmeow oficial (tulir/whatsmeow) no sha de
`UPSTREAM.txt`. Serve só para gerar uma imagem de teste; nada aqui é aplicado
pelos scripts `Apply-/Test-EvolutionPatches.ps1` (eles só leem `../patches/`).

## Arquivos

- `UPSTREAM.txt`: sha do tulir/whatsmeow usado (`5403881`, main de 02/11/2026).
- `101-go126-deps.patch`: `go.mod`/`go.sum` (resultado do `go mod tidy`, Go 1.26)
  e o `FROM golang:1.26-alpine` do estágio de build do `Dockerfile`.
- `102-whatsmeow-api.patch`: `pkg/user/service/user_service.go`
  (`UpdateBlocklist`/`GetBlocklist` com `dhash` vazio e
  `SetStatusMessage(types.SetStatusInput{Text: ...})`).
- `103-sqlstore-pool.patch`: o fork limitava o pool do Postgres dentro de
  `sqlstore.New`; o oficial não limita. Agora o Evolution abre o `sql.DB`
  (mesmo driver e DSN), aplica 25 abertas / 5 ociosas / vida 5 min / ociosa
  1 min (só Postgres, como o fork), cria o container com
  `sqlstore.NewWithDB` e roda `Upgrade`. O `defer container.Close()` do patch
  001 continua fechando o `sql.DB`. Arquivo novo
  `pkg/whatsmeow/service/auth_store.go` e testes.
- `prepare.sh`: monta a árvore de teste.

Os patches 012–018 não são usados: tudo o que eles trazem já está no sha
(`a238105`, `a7ea563`, `f331563`, `bb54a07`, `415df31`, `f376da2`, `098bcf7`,
`563bcaa`, `19c6103`, `d24d079`, `bbe00cd`, e também `4fa3462`/`28bfe53`).

Patch 008: a linha `store.BaseClientPayload.UserAgent.AppVersion = ...` fica
redundante, porque o `SetWAVersion` oficial já faz isso (`a763037`). É
inofensiva e o 008 não foi alterado. A parte de `DeviceProps.Version` continua
sendo do próprio Evolution.

## Gerar a imagem de teste

Sempre numa CÓPIA da árvore (o script recusa o próprio repositório):

```sh
mkdir /tmp/evo-teste
git archive HEAD | tar -x -C /tmp/evo-teste
sh customizations/whatsmeow-upgrade/prepare.sh /tmp/evo-teste
docker build --platform linux/amd64 -t ghcr.io/allen-xavier/evolution-go:<tag-de-teste> /tmp/evo-teste
```

Testes (na cópia): `go build ./...` e
`go test -vet=off ./pkg/instance/service ./pkg/whatsmeow/service ./pkg/events/rabbitmq ./pkg/sendMessage/service`
em `golang:1.26-alpine` (instalar antes `git build-base libjpeg-turbo-dev libwebp-dev`).
Com `EVOLUTION_TEST_POSTGRES_DSN` o teste do 103 também roda as migrações num
Postgres de verdade.

## O que muda no comportamento

- LID como padrão no envio para conversas individuais (`4f8f64e`): antes só
  trocava telefone por `@lid` quando a conta já tinha migrado
  (`LIDMigrationTimestamp`); agora troca sempre. Sem o LID no mapa, busca com
  `GetUserInfo`; se essa busca falhar, o envio falha.
- `IsOnWhatsApp` (`6e4c341`) passa a consultar com `addressing_mode=lid` e devolve o `@lid`
  em `JID` (o telefone vai em `PhoneNumber`). No Evolution isso aparece em
  `/user/check` (`jid` e `remoteJid` passam a ser `...@lid`, e `lid` tende a vir
  vazio) e no `remoteJID` usado pelo envio com conferência de número.
  Conferir o conector Chatwoot (botão "+" do XavierChat) antes de produção.
- Conferência de hash da mídia baixada (`6a7ac99`, `52afebe`, `d707fc2`):
  arquivo com hash diferente vira erro (`hash of media plaintext doesn't
  match`) em vez de seguir. O patch 011 não repete esse erro.
- Banco de autenticação: migrações 14, 15 e 16 do sqlstore (tabela
  `whatsmeow_nct_salt`, colunas `companion_meta_nonce` e
  `wasa_root_secret_id`), marcadas como compatíveis com v8+. Testado num
  Postgres 16: banco criado pelo fork (v13) sobe para v16, e o whatsmeow do
  fork volta a abrir o banco em v16 sem erro.
- JSON da blocklist (`GET /user/blocklist`, `POST /user/block` e `/user/unblock`): era
  `{"DHash", "JIDs": [...]}`, passa a ser
  `{"AddressingMode", "DHash", "Items": [{"LID", "PN", "Active"}]}`.

## Como voltar

Basta voltar a imagem anterior (ex.: `proxy-safety-v12-20261102`) no Portainer.
As migrações 14–16 só acrescentam tabela e colunas com valor padrão e são
compatíveis com v8+, então o fork (que conhece até a v13) continua abrindo o
banco. Nada do repositório muda: esta pasta não é usada pelos scripts dos
patches.

## 104 — telefone continua no `jid` da consulta

O whatsmeow oficial (`6e4c341`) devolve o LID em `IsOnWhatsAppResponse.JID` e o telefone em
`PhoneNumber`. Para o conector e o envio continuarem recebendo o telefone:

- `/user/check` mantém o telefone em `jid` e `remoteJid` e põe o LID em `lid`;
- `checkSingleUserExists` (envio) usa o telefone (`phoneJIDForCheck`).

Teste: `pkg/sendMessage/service/check_phone_test.go`.
