# middlewarePubSub

Middleware Publish/Subscribe em Go com dois brokers (A e B), feito para a
disciplina de Programação Distribuída (IFPB). A especificação está em
`docs/Etapa1-middlewarePubSub.pdf`.

Portas de cada broker:

- 7000/TCP: comandos dos clientes (sessão, tópicos, inscrições);
- 7001/UDP: eventos;
- 7002/TCP: canal entre os brokers.

Comandos e sincronização entre os brokers usam TCP; os eventos usam UDP,
com cabeçalho binário fixo de 34 bytes e até 1.024 bytes de conteúdo. Um
evento só é encaminhado ao outro broker quando ele tem inscritos no tópico.

## Organização

| Pasta | Conteúdo |
|---|---|
| `proto` | mensagens Protobuf (cliente e broker, broker e broker) |
| `internal/wire` | enquadramento por tamanho das mensagens no TCP |
| `internal/eventwire` | cabeçalho UDP dos eventos, filas limitadas e rodízio entre publicadores |
| `topic` | tópicos, gerações e catálogo versionado |
| `peer` | canal entre brokers: snapshot, replicação e heartbeat |
| `broker` | servidor do broker: TCP de comandos, UDP de eventos, cotas e encaminhamento ao par |
| `client` | biblioteca cliente, com troca de broker em caso de falha |
| `diagnostics` | contadores, cotas por identidade e dados do GetStats |
| `cmd/broker` | executável do broker |
| `cmd/leilao` | aplicação de exemplo: leiloeiro, participante e espectador |
| `cmd/sonda` | sonda dos ensaios de falha (canal de controle) |
| `cmd/ensaio` | ensaio de carga local com TCP e UDP reais |
| `scripts` | roteiros dos ensaios de falha e medição de MTU |
| `docs` | decisões de implementação, ensaios e resultados |

## Compilar e testar

Requer Go 1.23. Para regenerar o Protobuf, também `protoc` e
`protoc-gen-go`.

```
make build      # bin/broker, bin/sonda e bin/leilao
make test       # go test -race ./...
make proto      # regenera internal/pb
```

## Executar os dois brokers

Copie `config/credentials.example.json` para `config/credentials.json` e
troque os tokens. Depois, em dois terminais:

```
# B espera o A no canal entre brokers
bin/broker -id B -listen :7100 -udp-port 7101 -peer-id A -peer-listen :7002 -peer-token segredo

# A disca o B
bin/broker -id A -peer-id B -peer-admin -peer-addr 127.0.0.1:7002 -peer-token segredo
```

`bin/broker -h` lista as demais opções. Sem `-peer-id`, o broker roda
sozinho.

No macOS, a porta 7000 costuma estar ocupada pelo Receptor AirPlay. Desative
o receptor nos Ajustes do Sistema ou use outra porta no A (`-listen :7200`).

## Leilão

A aplicação de exemplo segue o cenário da seção 2 do documento: uma sala
com os tópicos `sala-1/lances`, `sala-1/reacoes` e `sala-1/placar`. Em
terminais separados, com os brokers rodando:

```
bin/leilao leiloeiro    -brokers 127.0.0.1:7000,127.0.0.1:7100 -id leiloeiro -token <token> -criar
bin/leilao participante -brokers 127.0.0.1:7000,127.0.0.1:7100 -id participante-1 -token <token>
bin/leilao espectador   -brokers 127.0.0.1:7100,127.0.0.1:7000 -id espectador-1 -token <token>
```

O leiloeiro cria os tópicos, acompanha os lances e publica o placar a cada
100 ms. O participante envia lances acima do placar e reações, com rajadas.
O espectador registra, a cada 5 s, o que recebeu de cada publicador:
eventos, perdas, latência e lacunas. `bin/leilao <papel> -h` lista as
taxas e os demais parâmetros.

## Biblioteca cliente

```go
c, err := client.Dial(ctx, client.Config{
	Brokers:  []string{"127.0.0.1:7000", "127.0.0.1:7100"},
	Identity: "espectador-1",
	Token:    "...",
})
sub, err := c.Subscribe(ctx, "sala-1/lances")
for ev := range sub.Events() {
	// ev.Payload
}
```

A biblioteca abre o UDP de eventos numa porta livre; para escolher a
porta, passe `DataPlane: dp` com `dp, _ := client.ListenUDPDataPlane(":7300")`.
Conteúdo acima de 1.024 bytes não é enviado e volta com INVALID_SIZE. Se o
broker cair, a biblioteca passa para o próximo da lista, mantém a sessão,
troca o destino UDP e restaura as inscrições.

## Ensaios

Os ensaios de falha rodam em contêineres (`make imagem`, depois
`scripts/ensaio.sh cenario <n>`). Os ensaios de carga com o leilão nos
contêineres rodam com `scripts/carga.sh <nome>`, e o ensaio de carga local
com `go run ./cmd/ensaio`. A falha de host em dois computadores usa
`scripts/duas-maquinas.sh`. Procedimentos estão em `docs/ensaios.md` e os
resultados de carga, MTU, RTT/2 e lacuna de failover em
`docs/resultados-marco4.md`. As decisões tomadas durante a implementação
estão em `docs/decisoes.md`.
