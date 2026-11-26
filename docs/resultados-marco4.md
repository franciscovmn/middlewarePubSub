# Resultados locais do Marco 4

Execuções feitas em 20/11/2026 no Windows, com brokers, publisher e subscriber
no mesmo host. Os dados de carga e gráficos brutos estão em
`ensaios/resultados/marco4-nominal-final` e
`ensaios/resultados/marco4-saturacao-final` (diretório ignorado pelo Git).

## Carga e limites por identidade

| Cenário | Alvo | Entregues/solicitados | Admitidos | Limitados | Vazão de solicitações | Latência média / p95 / p99 |
|---|---:|---:|---:|---:|---:|---:|
| Nominal | 20 eventos/s | 40/40 | 40 | 0 | 18,59 eventos/s | 0,044 / 0,052 / 1,000 ms |
| Saturação | 200 eventos/s | 69/200 | 69 | 131 | 167,29 solicitações/s | 0,008 / 0,029 / 0,036 ms |

No cenário saturado, a cota aceitou 69 eventos em aproximadamente 1,2 s e
rejeitou 131 com `LIMIT_EXCEEDED` (`rate_limited=131`). Isso corresponde à
rajada inicial mais a reposição de 50 eventos/s. Os dois cenários tiveram
zero descarte da fila do subscriber. O heap Go alocado cresceu 414.864 bytes
na execução nominal e 484.800 bytes na saturada. CPU de usuário foi
0,015625 s na nominal; na saturada ficou abaixo da resolução reportada pelo
contador do Windows (0,000000 s), portanto não deve ser interpretada como
custo zero.

## MTU

A interface Wi-Fi informa MTU configurada de 1500 bytes. A sonda IPv4 com DF
até o gateway `192.168.1.1` recebeu resposta para payload ICMP de até 1472
bytes, estimando MTU do caminho até esse gateway em 1500 bytes e payload UDP
IPv4 de até 1472 bytes sem fragmentação. A varredura do middleware aceitou
todos os tamanhos testados entre 0 e 1024 bytes. A medição até o gateway não
estabelece a MTU de um caminho até outro host ou broker.

## RTT/2 e failover

Com atraso de 30 ms em cada sentido no TCP do enlace entre brokers, o RTT
observado foi 61,098 ms. O encaminhamento descontou 30,549 ms do TTL, em
linha com RTT/2. Com publicação contínua e encerramento do broker ativo, a
lacuna observada entre o último evento anterior e o primeiro posterior foi
40,366 ms (20,008 ms até a falha e 20,358 ms entre a falha e o primeiro
evento após reconexão).

## Limites e gargalos observados

- A cota de eventos por identidade é o limite claro no cenário de 200/s;
  latência de admissão não foi o gargalo nesses ensaios locais.
- A carga, CPU e heap são medidos no processo de teste local, que contém os
  dois clientes e o broker; não representam três hosts ou a implantação
  isolada em produção.
- O teste de RTT injeta atraso no enlace TCP de metadados. A sonda de MTU
  alcança somente o gateway. A falha testada é encerramento de processo, não
  perda física de host ou partição de rede.
- O cenário de saturação não excedeu a fila do subscriber, então não mediu
  descarte por consumidor lento.

## Ensaios de carga com o leilão em contêineres

Execuções feitas em 26/11/2026 com `scripts/carga.sh`, no Docker de uma
máquina virtual Linux com 6 CPUs sobre o macOS, com um núcleo por broker
(`A_CPUS=1`, `B_CPUS=1`). A carga de referência é o leilão sem as sondas:
o leiloeiro, que publica o placar a cada 100 ms e acompanha os lances;
quatro participantes com 20 lances e 4 reações por segundo cada, sem
rajadas, durante 60 s; e dois espectadores, inscritos nos três tópicos. Os
participantes ímpares, o leiloeiro e o espectador-a preferem o A; os pares
e o espectador-b preferem o B. Cada espectador recebe, portanto, metade dos
publicadores pelo encaminhamento entre brokers. O limite de distribuição é
o padrão de 320 datagramas por segundo por broker (D87). Os datagramas são
contados com `iptables` em cada contêiner, numa janela de 30 s; a CPU, com
`docker stats`. Os registros ficam em
`ensaios/resultados/<data>-carga-<nome>` (diretório ignorado pelo Git).

| Ensaio | Datagramas UDP/s (A / B) | CPU (A / B) | Lances: latência média, perda | Reações: latência média, perda |
|---|---:|---:|---:|---:|
| Dois brokers | 324 / 214 | 4,6% / 3,7% | 0,71 ms, 0% | 0,56 ms, 0% |
| Um broker | 362 / - | 6,5% a 12,2% / - | 216,6 ms, 0,9% | 195,2 ms, 12,4% |
| Consumidor lento no espectador-b | 323 / 213 | 4,8% / 4,2% | espectador-a: 0,6 ms, 0% | espectador-a: 0,5 ms, 0% |

Latências e perdas são do espectador-a, média ponderada pelos eventos
recebidos; a perda vem das lacunas na sequência de cada publicador.

**Encaminhamento entre A e B.** Com dois brokers, os dois espectadores
receberam os 1.199 lances de cada um dos quatro participantes, sem perda,
tanto os publicados no próprio broker quanto os encaminhados pelo par. A
maior latência observada foi 17,7 ms, no placar do espectador-b.

**Benefício do segundo broker.** Com a mesma carga num só broker (A, sem
par configurado), o A precisa enviar 362 datagramas por segundo, acima do
limite de 320. Os eventos passam a esperar na fila de distribuição: a
latência média dos lances sobe de 0,7 ms para 217 ms, com máximas perto da
validade de 500 ms, e 12,4% das reações expiram antes de sair. Com dois
brokers, cada um atende só os seus clientes, e o mais carregado fica em
324 datagramas por segundo. A CPU não foi o gargalo: ficou abaixo de 13%
em todos os ensaios. O ganho vem da divisão do limite de distribuição
entre os brokers. Figura:
[carga-topologia.svg](figuras/carga-topologia.svg).

**Consumidor lento.** Com o espectador-b atrasado em 50 ms por evento, a
fila da inscrição dele descartou 3.360 eventos, e a latência dos lances
nele chegou a 3,2 s em média. O espectador-a, nos mesmos brokers e com a
mesma carga, não teve descarte, e a latência ficou em 0,6 ms; os brokers
enviaram os mesmos datagramas por segundo que no ensaio de referência. O
consumidor lento não afeta o broker nem os outros consumidores, porque a
fila fica na biblioteca cliente (D85). Os eventos que chegam ao espectador
lento, porém, já estão vencidos: a biblioteca entrega os que estão na fila
mesmo depois da validade.

## Justiça do rodízio entre publicadores

Para saturar a distribuição, o ensaio de justiça baixa o limite para 150
datagramas por segundo e usa cinco participantes só com lances: quatro a
10 por segundo e o participante-5 a 45 por segundo. O participante-5
publica no A, junto com o participante-1, o participante-3 e o leiloeiro.

O primeiro ensaio revelou dois defeitos no escalonador, já corrigidos:

- um publicador que esvaziava a fila e voltava a publicar entrava no fim
  da lista do rodízio, onde podia ficar sob o cursor e ser atendido de
  novo antes dos outros. Os publicadores leves, que chegam o tempo todo,
  furavam assim a vez do pesado, que quase não era atendido: a fila dele
  enchia até 1.024 eventos e expirava inteira;
- um evento vencido na frente da fila de um publicador gastava a vez dele
  no rodízio.

Lances entregues ao espectador-a, depois das correções:

| Publicador | Broker | Taxa pedida | Entregues ao espectador-a | Perda |
|---|---|---:|---:|---:|
| participante-1 | A | 10/s | 10,0/s | 0% |
| participante-3 | A | 10/s | 10,0/s | 0% |
| participante-5 | A | 45/s | 5,7/s | 87% |
| participante-2 | B, encaminhado | 10/s | 9,1/s | 9% |
| participante-4 | B, encaminhado | 10/s | 0,6/s | 94% |

Figura: [carga-justica.svg](figuras/carga-justica.svg).

Os publicadores leves do próprio broker ficam protegidos do pesado, que é o
objetivo do rodízio. Dois efeitos impedem uma divisão justa da sobra:

- os eventos vindos do par entram no A numa única fila, a do broker B.
  Todos os publicadores do B dividem uma vez do rodízio no A, e por isso o
  participante-2 e o participante-4 perdem eventos no espectador-a; no
  espectador-b, que está no B, não perdem nada;
- sob sobrecarga contínua, a fila de um publicador acumula eventos, e o
  escalonador sempre atende o mais antigo ainda válido. Ele sai perto do
  fim da validade, e muitos expiram enquanto esperam a vez de envio para
  cada inscrito, depois de já terem gasto parte da cota de datagramas. Por
  isso o participante-5 entrega 5,7 eventos por segundo, e não a sobra de
  cerca de 14 por segundo que o rodízio lhe daria.

## Lacuna de eventos no failover

A lacuna de eventos por fluxo no failover do UDP foi medida com o leilão no
cenário 8 (`docs/ensaios.md`). Com o broker encerrado
(`kill -9` ou parada), não houve lacuna acima da cadência dos fluxos. Com o
broker travado ou isolado, a lacuna foi de 4,2 s a 5,2 s, com os eventos
desse período perdidos. O valor local de 40,366 ms acima é de uma falha de
processo com um único publicador.

Gráficos produzidos pelos ensaios:

- Nominal: [marco4-nominal.svg](figuras/marco4-nominal.svg).
- Saturação: [marco4-saturacao.svg](figuras/marco4-saturacao.svg).
- Um broker contra dois: [carga-topologia.svg](figuras/carga-topologia.svg).
- Justiça: [carga-justica.svg](figuras/carga-justica.svg).

Os testes automatizados do projeto passaram com `go test ./... -count=1`.
