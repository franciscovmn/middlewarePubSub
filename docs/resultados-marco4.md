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

## Ensaios de carga ainda não medidos

Os resultados acima são exclusivamente dos ensaios locais com um broker no
mesmo processo/host dos clientes. Eles não permitem concluir sobre a
distribuição com dois brokers, justiça entre publicadores ou isolamento de
consumidor lento. Esses cenários não foram executados; portanto, não há
valores ou gráficos válidos para reportar nessas comparações.

| Medida pendente | Estado | O que falta para concluir |
|---|---|---|
| Carga com encaminhamento A↔B | Não medida | Executar a aplicação com clientes conectados aos dois brokers e coletar os fluxos locais e encaminhados. |
| Justiça do rodízio entre publicadores | Não medida | Rodar vários publicadores simultâneos, contabilizar eventos por publicador e comparar a distribuição (por exemplo, índice de Jain). |
| Descarte por consumidor lento | Não medido | Comparar a mesma carga com consumidor normal e com processamento atrasado, registrando descartes por inscrição. |
| Lacuna de eventos no failover UDP em contêineres | Não medida neste relatório | Executar o cenário 8 com o leilão e registrar a lacuna por fluxo; o valor local de 40,366 ms acima não substitui essa medição. |
| Benefício do segundo broker na distribuição | Não avaliado | Comparar coletas equivalentes com um e dois brokers, mantendo taxa, duração, publicadores e assinantes constantes. |

As ferramentas e os procedimentos documentados em `docs/ensaios.md` permitem
coletar essas medidas, mas a existência do roteiro não é resultado
experimental. Esta seção deve ser atualizada com os logs e os gráficos de
uma execução real antes de afirmar justiça, descarte observado ou melhora
da distribuição pelo segundo broker.

Gráficos produzidos pelo ensaio:

- Nominal: [marco4-nominal.svg](figuras/marco4-nominal.svg).
- Saturação: [marco4-saturacao.svg](figuras/marco4-saturacao.svg).

Os testes automatizados do projeto passaram com `go test ./... -count=1`.
