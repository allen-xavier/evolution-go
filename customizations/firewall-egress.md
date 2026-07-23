# Bloqueio de saída direta do container Evolution

O patch `fail-closed` é a primeira barreira. A segunda deve impedir, na rede, que
o container da Evolution alcance a internet sem passar pelo provedor de proxy.

## Política

Aplicar somente ao serviço/container `evolution_go`:

- permitir conexões já estabelecidas;
- permitir PostgreSQL, RabbitMQ e demais destinos internos necessários;
- permitir DNS para o resolvedor usado pelo Docker;
- permitir somente os IPs oficiais do gateway do provedor e a porta do proxy;
- rejeitar toda outra saída para a internet.

Não aplique a mesma regra ao `chatwoot_connector`: ele precisa acessar Chatwoot,
o serviço de consulta de IP e `web.whatsapp.com` para os testes.

## Antes de criar regras

Confirme com o provedor:

- lista fixa de IPs/CIDRs do gateway;
- porta e protocolo;
- se o hostname pode mudar de IP;
- disponibilidade de allowlist estável.

Uma regra baseada apenas no IP resolvido hoje pode interromper todas as
instâncias quando o provedor alterar o DNS.

Em Docker comum, a filtragem costuma ser feita na cadeia `DOCKER-USER`. Em Docker
Swarm/overlay, a regra depende da interface e do nó onde a tarefa está rodando.
Por isso, não há um comando universal seguro neste repositório. Antes da
implantação, registre:

```text
sub-rede/endereços do serviço Evolution:
destinos internos permitidos:
resolvedor DNS:
CIDRs do provedor de proxy:
porta do proxy:
Docker Compose ou Swarm:
distribuição Linux e firewall (iptables/nftables):
```

Depois de aplicar a política, valide duas situações:

1. proxy válido: a instância conecta e o painel mostra o IP do proxy;
2. proxy inválido: a instância fica desconectada e uma tentativa direta a
   `web.whatsapp.com:443` a partir do container falha.

Não considere a proteção concluída enquanto os dois testes não passarem.
