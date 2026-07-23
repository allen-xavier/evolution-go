# Migração segura para stacks separadas

O objetivo é impedir que a opção de republicar imagens do Portainer reinicie
RabbitMQ ou PostgreSQL durante uma atualização da Evolution.

## Antes da migração

Faça backup do volume `evolution_postgres_data`. Não execute dois containers de
PostgreSQL ou dois containers de RabbitMQ usando o mesmo volume ao mesmo tempo.

## Ordem da migração

1. Pause o atendimento e confirme que não existem envios em andamento.
2. Remova da stack atual apenas os serviços `evolution_go` e
   `chatwoot_connector`.
3. Remova os serviços antigos `evolution_postgres` e `evolution_rabbitmq`, sem
   excluir os volumes externos.
4. Crie a stack de infraestrutura com
   `docker-stack.infrastructure.swarm.yml`, reutilizando as mesmas senhas e os
   mesmos volumes externos.
5. Aguarde PostgreSQL e RabbitMQ ficarem `healthy`.
6. Crie a stack de aplicação com `docker-stack.application.swarm.yml`.

Use estas imagens na stack da aplicação:

```text
EVOLUTION_IMAGE=ghcr.io/allen-xavier/evolution-go:proxy-safety-v4-20260723
CONNECTOR_IMAGE=ghcr.io/allen-xavier/evolution-go-chatwoot-connector:0.5.3
```

Após a separação, atualizações normais devem ser feitas somente na stack de
aplicação. A opção de republicar imagens não alcançará banco ou RabbitMQ.

As imagens da infraestrutura estão fixadas por digest. Uma troca futura de
versão deve ser realizada como manutenção independente, com backup e janela
planejada.
