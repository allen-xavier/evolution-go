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

## Atualização futura

Depois de colocar uma release nova no repositório, execute primeiro o teste. Se
`git apply --check` falhar, não gere nem publique a imagem: o patch precisa ser
revisado para o novo código.

O bloqueio de rede continua sendo necessário como segunda barreira. Consulte
`firewall-egress.md`.
