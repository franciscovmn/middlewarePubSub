# Ensaios de falha

Este documento descreve como executar os ensaios de tolerância a falhas
pedidos na seção 8 do documento do projeto, com os brokers A e B em
ambientes isolados. Os cenários 1 a 7 usam as sondas, que só usam o canal
de controle (TCP). O cenário 8 roda a aplicação do leilão sobre o plano de
dados UDP e mede a lacuna de eventos: do último evento antes da falha ao
primeiro depois.

## 1. Ambiente

```
rede "pubsub" (bridge)

  a (broker A)  ----- canal 7002 ----->  b (broker B)
  disca b:7002                           escuta :7002
  7000/TCP, 7001/UDP                     7000/TCP, 7001/UDP

  sonda-a: lista a,b (prefere A)         sonda-b: lista b,a (prefere B)
```

- Cada broker roda no próprio contêiner, com rede, CPU e memória
  próprias.
- A imagem única (`Dockerfile`) traz `broker`, `sonda` e `rede`. Este
  último injeta falhas com `iptables` e `tc` dentro do contêiner, por isso
  todos os serviços têm a capacidade `NET_ADMIN`.
- As sondas (`cmd/sonda`) são clientes da biblioteca que:
  - mantêm inscrições nos tópicos `sala-1/lances`, `sala-1/reacoes` e
    `sala-1/placar` (estado);
  - registram cada failover com a lacuna medida;
  - consultam o canal de comandos a cada 500 ms, registrando as transições
    entre disponível e indisponível.
- A aplicação do leilão (`cmd/leilao`) fica no perfil `leilao` do compose
  e só sobe no cenário 8 ou com `docker compose --profile leilao up`:
  - `leiloeiro`, no A, cria a sala, acompanha os lances e publica o
    placar;
  - `participante-1`, no A, e `participante-2`, no B, enviam lances e
    reações, com rajadas periódicas;
  - `espectador-a`, no A, e `espectador-b`, no B, recebem os três tópicos
    e registram cada fluxo (publicador e tópico).
- As credenciais de ensaio ficam em `config/credentials.ensaio.json`. O
  administrador é `leiloeiro`.

### Parâmetros

O `docker-compose.yml` lê os parâmetros de `scripts/ensaio.env`. Para outro
conjunto, copie o arquivo e use `ENSAIO_ENV=<cópia> scripts/ensaio.sh ...`
ou `docker compose --env-file <cópia> ...`.

| Variável | Padrão | Uso |
|---|---|---|
| `IMAGEM` | `pubsub-middleware:ensaio` | imagem dos serviços |
| `CREDENCIAIS` | `config/credentials.ensaio.json` | credenciais montadas nos brokers |
| `TOKEN_PAR` | `ensaio-par` | segredo do canal entre brokers |
| `A_PORTA_TCP`, `A_PORTA_UDP` | 7000, 7001 | portas do A publicadas no host |
| `B_PORTA_TCP`, `B_PORTA_UDP` | 7100, 7101 | portas do B publicadas no host |
| `A_CPUS`, `A_MEMORIA` | 1, 256m | limites do A |
| `B_CPUS`, `B_MEMORIA` | 1, 256m | limites do B |
| `DISTRIBUICAO`, `DISTRIBUICAO_RAJADA` | 320, 320 | limite de datagramas UDP enviados por segundo por broker e rajada (seção 7.2, D87) |
| `MEMORIA_MAX_BYTES` | 201326592 | alvo máximo do heap Go por broker (192 MiB); `A_MEMORIA`/`B_MEMORIA` são os tetos rígidos do contêiner |
| `LOG_FORMATO`, `LOG_NIVEL` | text, info | `json` facilita processar os registros nos ensaios de carga |
| `TOPICOS` | os três tópicos acima | tópicos das sondas (`nome=estado` para tópico de estado) |
| `SILENCIO` | 5s | silêncio que as sondas e o leilão tratam como falha |
| `SALA` | sala-1 | sala do leilão, prefixo dos tópicos |
| `PLACAR` | 100ms | intervalo entre publicações do placar |
| `LANCES`, `REACOES` | 5, 5 | lances e reações por segundo de cada participante |
| `RAJADA`, `RAJADA_CADA` | 10, 10s | reações enviadas de uma vez e intervalo entre rajadas |
| `RESUMO` | 2s | intervalo entre os resumos do leilão |
| `DURACAO` | 0 | tempo de execução dos participantes; 0 roda até o ambiente descer |
| `ATRASO_CONSUMIDOR` | 0 | atraso por evento nos espectadores, para simular consumidor lento |

Reaproveitamento nos ensaios de carga:

- **Geradores de carga fora do Docker** usam as portas publicadas (A em
  7000/7001, B em 7100/7101).
- **Geradores em contêiner** entram na rede `pubsub` e usam `a:7000` e
  `b:7000`.
- **Carga com o leilão:** suba o perfil `leilao` com taxas maiores
  (`LANCES`, `REACOES`, `PLACAR`) ou rode mais participantes com
  `leilao participante` em outros contêineres; os resumos de publicações e
  de fluxos trazem resultados de admissão, perdas, latência e descartes.
- **Perda e atraso de pacotes** vêm de `rede perda` e `rede atraso`, dentro
  de qualquer contêiner:
  ```
  docker compose exec a rede perda 0.05 udp     # 5% dos datagramas que chegam ao A
  docker compose exec b rede atraso 20 5        # 20 ms, variando 5 ms, em tudo o que sai do B
  docker compose exec a rede restaurar
  ```

### Comandos

```
scripts/ensaio.sh construir                 # ou make imagem
scripts/ensaio.sh cenario 3 explicito       # ou make ensaio CENARIO="3 explicito"
scripts/ensaio.sh todos                     # todos os cenários e variantes, cerca de 6 min
scripts/ensaio.sh descer
```

Cada cenário:

1. sobe o ambiente limpo e cria os tópicos;
2. espera as duas sondas se inscreverem;
3. marca o instante da injeção, injeta a falha e confere os resultados
   esperados;
4. guarda tudo em `ensaios/resultados/<data>-cenario<n>-<variante>/`.

| Arquivo | Conteúdo |
|---|---|
| `a.log`, `b.log`, `sonda-a.log`, `sonda-b.log` | registros completos de cada serviço; no cenário 8, também os do leilão |
| `marcas.log` | instantes marcados pelo roteiro (injeção, recuperação) |
| `comandos.log` | saída dos comandos avulsos do roteiro |
| `linha-do-tempo.log` | todas as linhas, ordenadas pelo horário, com a origem entre colchetes |
| `resumo.log` | só os eventos de falha, enlace, catálogo e inscrição |
| `verificacoes.log` | cada resultado esperado, como `ok` ou `divergente` |
| `medidas.log` | por cliente: injeção até a detecção, injeção até a restauração e a lacuna da biblioteca; por espectador e fluxo: a lacuna de eventos |

Uma verificação divergente não interrompe o cenário; os registros são
guardados mesmo assim. O diretório `ensaios/resultados/` fica fora do
controle de versão; copie para o relatório os resultados que interessarem.

## 2. Registros e medição

Todos os registros saem com horário UTC em milissegundos
(`time=2026-11-19T17:00:45.556Z`). No mesmo computador, os contêineres
compartilham o relógio do kernel, então os horários são comparáveis
diretamente. Em máquinas separadas, veja a seção 5.

Mensagens usadas na medição:

| Origem | Mensagem | Significado |
|---|---|---|
| ensaio | `marca texto="injeção: ..."` | instante zero, registrado logo antes do comando de injeção |
| sonda | `client: falha no canal de controle` | detecção da falha pelo cliente, com a causa |
| sonda | `failover concluído` | inscrições restauradas; traz `deteccao`, `restauracao` e `lacuna_ms` |
| sonda | `inscrição encerrada` | inscrição fechada, com o erro de `Err()` |
| sonda | `comandos indisponíveis` / `comandos disponíveis` | transições do canal de comandos, com resolução igual ao intervalo de sondagem (500 ms) |
| broker | `enlace: perdido` | queda do canal entre brokers, com a causa (`par em silêncio por 5s` ou `conexão encerrada: ...`) |
| broker | `enlace: ativo` | canal entre brokers pronto após o handshake |
| broker | `enlace: snapshot do catálogo enviado/aplicado` | snapshot trocado, com versão, ativos e apagados |
| broker | `enlace: catálogo adotado do par` | A sem catálogo adotou o do B |
| broker | `enlace: interesse do par recebido` | snapshot de interesse, com o número de tópicos |
| broker | `inscrição restaurada` | Subscribe de restauração aceito, com `enlace_ativo` |
| broker | `sessão encerrada` | fim de uma sessão de cliente, com a causa |
| broker | `alteração do catálogo não aplicada` | CreateTopic ou DeleteTopic recusado por falta do par |
| leilão | `failover concluído` | o mesmo da sonda, para cada papel do leilão |
| espectador | `fluxo` | por publicador e tópico, a cada resumo: recebidos, perdidos pela sequência, latência média, p95 e máxima, cadência e maior intervalo |
| espectador | `lacuna de eventos` | intervalo acima de 1,5 vez a cadência do fluxo, com o último evento antes (`de`), o primeiro depois (`ate`) e os perdidos |
| espectador | `resumo` | eventos recebidos e descartados nas filas das inscrições |
| leiloeiro, participante | `publicações` | por tópico, a cada resumo: enviadas, resultados de admissão e recusas na hora (`erro_reconectando`, por exemplo) |

A lacuna de controle tem três medidas:

- **injeção até detecção:** o tempo que o cliente leva para perceber a
  falha. É próximo de zero quando o TCP é encerrado e de até 5 s quando a
  falha é silenciosa.
- **detecção até restauração:** a lacuna da biblioteca (`lacuna_ms`),
  formada pela nova conexão, pelo Hello e pelos Subscribe de restauração.
- **injeção até restauração:** a interrupção total vista pela aplicação.

A lacuna de eventos é medida no espectador, por fluxo:

- A **cadência** é o maior dos últimos 20 intervalos normais entre eventos
  do fluxo. O máximo, e não a média, evita que as rajadas de reações façam
  um intervalo comum parecer lacuna.
- Uma **lacuna** é um intervalo acima de 1,5 vez a cadência e com ao menos
  20 ms a mais. Ela não entra na cadência.
- A medida é a maior lacuna que cobre o intervalo entre as marcas
  `injeção:` e `injeção concluída`. Sem lacuna, a interrupção ficou abaixo
  da resolução do fluxo, que é a própria cadência.
- A latência compara o horário de envio, que vai no conteúdo do evento,
  com o de chegada. Em máquinas separadas, ela inclui a diferença entre os
  relógios.
- As perdas são os saltos na sequência do publicador. Incluem eventos
  recusados na admissão, perdidos na rede e descartados nas filas.

Ressalvas:

- A marca é registrada antes do comando de injeção. O tempo do próprio
  `docker` (cerca de 60 a 80 ms neste ambiente) entra na medida de
  detecção.
- Nas falhas silenciosas, o silêncio conta a partir da última mensagem
  recebida. Por isso a detecção pode ocorrer um pouco antes de 5 s depois
  da marca.
- Linhas no mesmo milissegundo não têm ordem causal garantida na linha do
  tempo.

## 3. Formas de injeção

| Variante | Mecanismo | O que o outro lado observa | Aproxima |
|---|---|---|---|
| `matar` | `docker compose kill -s KILL` | o kernel fecha os sockets: FIN ou RST imediato | queda do processo |
| `parar` | `docker compose stop` (SIGTERM) | o broker encerra as conexões | parada administrativa |
| `travar` | `docker compose pause` | silêncio; o kernel ainda confirma o TCP | processo travado |
| `isolar` | `docker network disconnect` | silêncio; nada chega nem sai | queda do host ou cabo desconectado |
| `rede cortar <host> silencioso` | `iptables DROP` na entrada, aplicado nos dois lados | silêncio | partição com perda de pacotes |
| `rede cortar <host> explicito` | `iptables REJECT --reject-with tcp-reset` na entrada e na saída, aplicado num lado | RST imediato nos dois sockets | partição com encerramento das conexões |

## 4. Cenários

Em todos os cenários, antes da injeção:

- o enlace entre A e B está ativo;
- os três tópicos existem;
- a sonda-a está inscrita pelo A, e a sonda-b, pelo B.

### Cenário 1: falha do broker do cliente (A)

**Procedimento:** `scripts/ensaio.sh cenario 1 <matar|parar|travar|isolar>`.
Manualmente:

```
docker compose kill -s KILL a              # ou stop, pause, network disconnect
docker compose exec sonda-b sonda criar -brokers b:7000 -id leiloeiro -token ensaio-leiloeiro -topico sala-9/teste
docker compose exec sonda-b sonda inscrever -brokers b:7000 -topico sala-1/lances
docker compose exec sonda-b sonda consultar -brokers b:7000 -topico sala-1/lances
docker compose start a                     # ou unpause, network connect --alias a
```

**Esperado (seção 8, "Broker do cliente falha"):**
- A sonda-a reconecta ao B e restaura as inscrições, que o B aceita com o
  enlace fora (`enlace_ativo=false`, D58).
- Nenhuma inscrição é encerrada, e a sonda-b não é afetada.
- No B, criação de tópico e inscrição nova retornam UNAVAILABLE (estado
  global indisponível), e a consulta ao catálogo continua respondendo.
- O B registra `enlace: perdido`.
- Quando o A volta, o enlace se restabelece. O A, sem catálogo, adota o do
  B (cenário 7).

**Registrar:**
- injeção até a detecção e até a restauração, e a lacuna da biblioteca
  (`medidas.log`);
- a causa da queda vista pela sonda e pelo B;
- as respostas dos comandos.

### Cenário 2: falha do broker do cliente (B)

**Procedimento:** `scripts/ensaio.sh cenario 2 <matar|parar|travar|isolar>`;
manualmente, como no cenário 1, trocando `a` por `b` e `sonda-b` por
`sonda-a`.

**Esperado (seção 8, "Broker do cliente falha" para a sonda-b e "Broker par
falha" para o A):**
- A sonda-b passa ao A e restaura as inscrições.
- A sonda-a continua no A sem interrupção, porque a entrega local continua.
- No A, criação de tópico e inscrição nova retornam UNAVAILABLE, porque a
  sincronização com o par está indisponível.
- Quando o B volta, recebe o snapshot do catálogo e o interesse
  (cenário 6).

**Registrar:** o mesmo do cenário 1.

### Cenário 3: enlace entre brokers interrompido

**Procedimento:** `scripts/ensaio.sh cenario 3 <silencioso|explicito>`.
Manualmente:

```
# silencioso: descarte nos dois lados
docker compose exec a rede cortar b silencioso
docker compose exec b rede cortar a silencioso
# explícito: RST nos dois sentidos, aplicado só no A
docker compose exec a rede cortar b explicito

docker compose exec sonda-a sonda criar -brokers a:7000 -id leiloeiro -token ensaio-leiloeiro -topico sala-9/teste
docker compose exec sonda-b sonda inscrever -brokers b:7000 -topico sala-1/lances

docker compose exec a rede restaurar
docker compose exec b rede restaurar
```

**Esperado (seção 8, "Enlace entre brokers interrompido" e "Broker
retorna"):**
- Os dois brokers registram `enlace: perdido`: cerca de 5 s depois do corte
  silencioso (`par em silêncio por 5s`) ou de imediato no explícito
  (`connection reset by peer`).
- Criação de tópico no A e no B e inscrição nova retornam UNAVAILABLE.
- As sondas não trocam de broker, e as inscrições existentes continuam
  ativas: cada lado opera localmente.
- Na restauração, o A reconecta. O B recebe novo snapshot do catálogo, os
  dois trocam o snapshot de interesse, e a criação de tópico volta a
  funcionar.

**Registrar:**
- o tempo do corte até `enlace: perdido` em cada broker;
- o tempo da restauração até `enlace: ativo`, que inclui o recuo das
  tentativas de reconexão;
- as respostas dos comandos.

### Cenário 4: partição entre cliente e broker

**Procedimento:** `scripts/ensaio.sh cenario 4 <silencioso|explicito>`.
Manualmente:

```
# silencioso
docker compose exec sonda-a rede cortar a silencioso
docker compose exec a rede cortar sonda-a silencioso
# explícito
docker compose exec sonda-a rede cortar a explicito

docker compose exec sonda-a rede restaurar
docker compose exec a rede restaurar
```

**Esperado (seção 8, "Broker do cliente falha", do ponto de vista do
cliente):**
- A sonda-a detecta a falha, em até 5 s ou de imediato, passa ao B e
  restaura as inscrições. Como o enlace está ativo, o interesse é anunciado
  ao A.
- O A encerra a sessão da sonda-a (`cliente em silêncio por 5s` ou
  `conexão encerrada`).
- O enlace entre os brokers não é afetado.
- Depois da restauração da rede, a sonda-a continua no B: não há retorno
  automático (D60).

**Registrar:**
- as medidas da sonda-a;
- o instante em que o A encerrou a sessão, relativo à injeção;
- a confirmação de que a sonda-a não voltou ao A.

### Cenário 5: os dois brokers falham e voltam um de cada vez

**Procedimento:** `scripts/ensaio.sh cenario 5 <a-primeiro|b-primeiro>`.
Manualmente:

```
docker compose kill -s KILL a b
docker compose start a                      # o primeiro a voltar
docker compose exec sonda-a sonda consultar -brokers a:7000 -topico sala-1/lances
docker compose exec sonda-a sonda criar -brokers a:7000 -id leiloeiro -token ensaio-leiloeiro -topico sala-1/lances
docker compose start b                      # o segundo
docker compose exec sonda-a sonda criar -brokers a:7000 -id leiloeiro -token ensaio-leiloeiro -topico sala-1/lances
docker compose exec sonda-a sonda criar -brokers a:7000 -id leiloeiro -token ensaio-leiloeiro -topico sala-1/reacoes
docker compose exec sonda-a sonda criar -brokers a:7000 -id leiloeiro -token ensaio-leiloeiro -topico sala-1/placar -tipo estado
```

**Esperado (seção 8, "Dois brokers falham"):**
- As duas sondas detectam a falha. As inscrições permanecem abertas
  enquanto a biblioteca tenta reconectar.
- **Com um só broker de volta, o serviço continua indisponível para
  tópicos e inscrições.** O broker aceita o Hello, mas responde UNAVAILABLE
  a consultas, inscrições e restaurações, porque o catálogo dele está vazio
  e não sincronizado (D57). Também não cria tópicos, pois as alterações
  administrativas exigem o enlace. As inscrições não são encerradas: um
  broker sem catálogo não pode afirmar que um tópico deixou de existir.
- Quando o segundo broker volta, o enlace se restabelece e o A adota o
  catálogo vazio do B. A restauração recebe TOPIC_NOT_FOUND, e as inscrições
  terminam com `ErrTopicDeleted`. Sem persistência, os tópicos precisam ser
  recriados; eles voltam com id 1 a 3 e geração 1, e as sondas se inscrevem
  de novo.

**Registrar:**
- o intervalo com um só broker e as respostas UNAVAILABLE;
- o instante do encerramento das inscrições;
- a recriação dos tópicos;
- a duração total sem serviço.

A "lacuna" que a sonda registra aqui inclui as esperas do roteiro e não
mede o sistema.

**Desvio do documento (D84):** a tabela da seção 8 diz "serviço
indisponível até ao menos um broker voltar". Aqui, um broker que volta
sozinho aceita conexões, mas o catálogo, as inscrições e a criação de
tópicos só voltam com os dois brokers. O desvio foi mantido de propósito e
é justificado no relatório.

### Cenário 6: retorno do broker

**Procedimento:** `scripts/ensaio.sh cenario 6`. Manualmente:

```
docker compose kill -s KILL b
docker compose start b
docker compose exec sonda-b sonda inscrever -brokers b:7000 -topico sala-1/lances
```

**Esperado (seção 8, "Broker retorna"):**
- A sonda-b passa ao A.
- Com o retorno do B, o A reconecta e envia o snapshot do catálogo
  (`ativos=3`), e o B o aplica.
- O A envia o interesse com os 3 tópicos, porque as duas sondas estão nele,
  e o B envia o interesse vazio.
- A sonda-b continua no A, sem retorno automático, e o B volta a aceitar
  inscrições novas.

**Registrar:**
- o tempo do início do B até `enlace: ativo`;
- as versões e as contagens dos snapshots;
- a confirmação de que não houve retorno automático.

### Cenário 7: reinício do A com adoção do catálogo

**Procedimento:** `scripts/ensaio.sh cenario 7`. O roteiro apaga
`sala-1/reacoes` antes da falha, para o catálogo ter um nome apagado.
Manualmente:

```
docker compose exec sonda-a sonda apagar -brokers a:7000 -id leiloeiro -token ensaio-leiloeiro -topico sala-1/reacoes
docker compose kill -s KILL a
docker compose start a
docker compose exec sonda-a sonda criar -brokers a:7000 -id leiloeiro -token ensaio-leiloeiro -topico sala-1/reacoes
docker compose exec sonda-a sonda criar -brokers a:7000 -id leiloeiro -token ensaio-leiloeiro -topico sala-2/lances
```

**Esperado (D54):**
- O A reiniciado registra `catálogo adotado do par` com 2 ativos e 1
  apagado, e só então o enlace fica ativo.
- O B não remove tópicos, e nenhuma inscrição das sondas é encerrada.
- Recriar `sala-1/reacoes` pelo A dá id 2 e geração 2, continuando a
  geração. Um tópico novo recebe o id 4.

**Registrar:**
- a versão adotada;
- as contagens de ativos e apagados;
- a ausência de encerramentos;
- a geração do tópico recriado.

### Cenário 8: falha do broker com o leilão em andamento

**Procedimento:** `scripts/ensaio.sh cenario 8 <matar|parar|travar|isolar>`.
O roteiro prepara o ambiente como nos outros cenários, sobe o leilão e
espera os eventos circularem pelos dois brokers antes de injetar a falha no
A. Manualmente:

```
docker compose --profile leilao up -d leiloeiro participante-1 participante-2 espectador-a espectador-b
docker compose exec sonda-b sonda marcar -texto "injeção: kill -9 no A"
docker compose kill -s KILL a
docker compose exec sonda-b sonda marcar -texto "injeção concluída"
docker compose start a
```

**Esperado (seção 8, "Broker do cliente falha" e "Broker par falha"):**
- Antes da falha, cada espectador recebe o participante do próprio broker
  e, por um único encaminhamento UDP, o do outro.
- O espectador-a, o participante-1 e o leiloeiro passam ao B e restauram
  as inscrições. Nenhuma inscrição é encerrada.
- O espectador-b e o participante-2 continuam no B. O espectador-b segue
  recebendo o participante-2 sem lacuna, porque a entrega local continua,
  e volta a receber o participante-1 e o placar quando eles passam ao B.
- A lacuna de eventos acompanha a detecção: com o processo morto, fica
  abaixo da cadência dos fluxos; com o broker travado ou isolado, dura
  cerca de 5 s nos fluxos que passavam pelo A, e os eventos desse período
  são perdidos, sem reprodução.
- Depois da recuperação do A, os clientes continuam no B (D60).

**Registrar:**
- por fluxo e espectador, a lacuna de eventos e os perdidos
  (`medidas.log`);
- as medidas de failover de cada papel;
- os resultados de admissão dos participantes durante a troca
  (`publicações`), incluindo as recusas com `erro_reconectando` e os
  UNKNOWN das publicações sem resultado.

## 5. Limitações e falha de host

Contêineres no mesmo computador validam o protocolo e a lógica, mas não
reproduzem falhas de hardware, de energia nem do sistema operacional:

- **Kernel compartilhado.** Todos os contêineres usam o mesmo kernel; no
  macOS e no Windows, o da máquina virtual do Docker Desktop. Uma pane do
  kernel, falta de energia ou travamento do host derruba os dois brokers
  juntos.
- **`travar` não é host travado.** Congela o processo, mas o kernel
  continua confirmando o TCP e mantém os sockets.
- **`isolar` não é cabo desconectado.** Remove a interface do contêiner,
  mas a rede bridge é virtual: não há latência física, perda de enlace nem
  a pilha de rede de outra máquina.
- **O RST do corte explícito é gerado pelo `iptables`.** Não vem de uma
  máquina que reiniciou e perdeu o estado das conexões.
- **O relógio é único.** Isso facilita a medição, mas esconde o efeito da
  diferença entre relógios.
- **CPU e memória são limitadas por cgroups,** não por hardware separado.

### Falha de host em duas máquinas

Para ensaiar falha de host, rode cada broker numa máquina física ou
virtual separada, com os clientes numa terceira máquina, ou nas duas.

1. **Sincronize os relógios** com NTP (`chrony`) nas máquinas. Confira o
   desvio com `chronyc tracking` antes e depois do ensaio, e anote-o no
   relatório: as medidas entre máquinas valem com essa margem.
2. **Suba os brokers** com os executáveis (`make build`) ou com a imagem,
   usando a rede do host:
   ```
   # máquina 2 (B)
   docker run --rm --network host --cap-add NET_ADMIN -v $PWD/config:/etc/pubsub:ro pubsub-middleware:ensaio \
     broker -id B -credentials /etc/pubsub/credentials.ensaio.json -peer-id A -peer-listen :7002 -peer-token ensaio-par 2> b.log
   # máquina 1 (A)
   docker run --rm --network host --cap-add NET_ADMIN -v $PWD/config:/etc/pubsub:ro pubsub-middleware:ensaio \
     broker -id A -credentials /etc/pubsub/credentials.ensaio.json -peer-id B -peer-admin -peer-addr <ip-de-B>:7002 -peer-token ensaio-par 2> a.log
   ```
   Com a rede do host, o endereço UDP do par é aprendido pelo IP real do
   enlace, sem `-peer-udp-addr`.
3. **Suba os clientes** na máquina dos clientes: as sondas, com
   `sonda observar -brokers <ip-de-A>:7000,<ip-de-B>:7000 ...` e o
   equivalente com B primeiro, e o leilão, com `leilao leiloeiro -criar`,
   dois `leilao participante` e dois `leilao espectador`, cada par com uma
   ordem de brokers. A porta UDP de cada cliente (`-udp`) precisa estar
   liberada no firewall para receber eventos.
4. **Marque e injete** na máquina dos clientes: registre a marca com
   `sonda marcar -texto "injeção: ..." 2>> marcas.log`, injete a falha no
   host alvo e registre logo depois a marca `injeção concluída`, que fecha
   o intervalo usado na lacuna de eventos. Formas de injeção:
   - cortar a energia ou desconectar o cabo de rede;
   - `sudo ip link set <interface> down`: o host some da rede sem fechar
     conexões;
   - `echo o | sudo tee /proc/sysrq-trigger`: desliga o kernel na hora, sem
     encerrar os processos (exige `kernel.sysrq` habilitado);
   - `echo b | sudo tee /proc/sysrq-trigger`: reinicia na hora.
5. **Colete:** copie os registros de cada máquina para um diretório, com os
   nomes `a.log`, `b.log`, `sonda-a.log`, `sonda-b.log` e `marcas.log`, e
   rode `scripts/linha-do-tempo.sh <diretório>` para obter a linha do
   tempo, o resumo e as medidas. Os resultados esperados são os dos
   cenários 1 e 2, com detecção silenciosa: cerca de 5 s, sem
   `conexão encerrada`.

## 6. Execução de referência

Execução em 19/11/2026, com Docker 29.5.2 no Docker Desktop (Linux arm64,
máquina virtual sobre macOS) e todos os cenários com as verificações em
`ok`. Os tempos são do mesmo computador e servem de referência para
comparação, não de resultado de host.

| Cenário | Variante | Injeção até detecção | Lacuna da biblioteca | Injeção até restauração |
|---|---|---|---|---|
| 1 (A falha) | matar | 69 ms | 1,4 ms | 70 ms |
| 1 | parar | 72 ms | 2,0 ms | 74 ms |
| 1 | travar | 4594 ms | 6,0 ms | 4600 ms |
| 1 | isolar | 4969 ms | 3,1 ms | 4972 ms |
| 2 (B falha) | matar | 69 ms | 1,8 ms | 71 ms |
| 2 | parar | 83 ms | 4,9 ms | 88 ms |
| 2 | travar | 4907 ms | 4,1 ms | 4911 ms |
| 2 | isolar | 4930 ms | 2,9 ms | 4933 ms |
| 4 (partição cliente) | silencioso | 5029 ms | 2,8 ms | 5032 ms |
| 4 | explicito | 83 ms | 0,9 ms | 84 ms |
| 6 (retorno do B) | matar | 63 ms | 1,2 ms | 64 ms |
| 7 (reinício do A) | matar | 72 ms | 1,2 ms | 74 ms |

No cenário 3, o enlace caiu 5,06 s depois do corte silencioso e entre 0,29
e 0,35 s depois do explícito; o tempo do explícito inclui a execução do
`docker compose exec`. Após a restauração da rede, o enlace voltou em
0,18 s no corte silencioso e em 2,0 a 2,2 s no explícito. No explícito, as
tentativas recusadas durante o corte fizeram o recuo crescer.

No cenário 5, as inscrições foram restauradas apenas depois do retorno do
segundo broker, e então encerradas por tópico inexistente. Os tempos desse
cenário refletem as esperas do roteiro.

No cenário 8 (execução em 24/11/2026, no mesmo ambiente), os papéis que
passaram ao B restauraram as inscrições entre 3 e 15 ms depois da
detecção. A lacuna de eventos por fluxo foi:

| Variante | Fluxos que passavam pelo A | participante-2 no espectador-a | participante-2 no espectador-b |
|---|---|---|---|
| matar | sem lacuna acima da cadência (100 a 200 ms) | sem lacuna | sem lacuna |
| travar | 5,1 a 5,2 s; 25 lances e 50 placares perdidos | 4,2 s | sem lacuna |
| isolar | 5,1 a 5,2 s; 25 lances e 50 placares perdidos | 4,6 s | sem lacuna |

Com o processo morto, o TCP é encerrado na hora, e a troca de broker cabe
entre dois eventos consecutivos. Com o broker travado ou isolado, a lacuna
é o silêncio de 5 s até a detecção mais a cadência do fluxo. O
participante-2 chega ao espectador-a pelo B; a lacuna dele termina quando
o espectador-a se reinscreve no B.

## 7. Carga local, MTU e dados para gráficos

`go run ./cmd/ensaio -eventos 100 -taxa 20 -saida ensaios/resultados/nominal`
levanta um broker e dois clientes locais usando TCP/UDP reais, cria um
tópico efêmero, publica na taxa indicada e grava `carga.csv`, `mtu.csv`,
`resumo.txt`, `mtu-interface.txt` e `metricas.svg`. O CSV inclui latência do
resultado de admissão, status, memória alocada observada, CPU de usuário e
descartes da inscrição. `mtu-interface.txt` registra a MTU configurada na
interface local disponível e o payload IPv4/UDP teórico sem fragmentação
(MTU menos 28 bytes). `mtu.csv` varre payloads de 0 a 1.024 bytes para medir
o limite aceito pelo codec UDP do middleware; esse ensaio não envia sondas
de caminho e não substitui uma medição de MTU entre hosts.

Em Windows, meça a MTU IPv4 do caminho até um destino que responda ICMP com
`powershell -ExecutionPolicy Bypass -File scripts/medir-mtu.ps1 -Destino <IPv4>`.
O script usa `ping -f` e busca o maior payload que recebe resposta. Timeout
ou filtragem ICMP tornam o resultado inconclusivo; o número só vale para o
caminho até o destino indicado.

Para uma amostra de saturação reproduzível, use
`go run ./cmd/ensaio -eventos 200 -taxa 200 -saida ensaios/resultados/saturacao`.
Os resultados de admissão `LIMIT_EXCEEDED` e os contadores `rate_limited`
mostram o descarte por cota. O resumo inclui média e percentis p50/p95/p99
da latência de admissão. Para medir RTT/2 e a lacuna de eventos no
failover sem Docker, execute
`go test ./client -run 'TestMeasure(EventGapAcrossBrokerFailover|RTTHalfTTLWithControlledDelay)' -count=1 -v`.
Esses testes sobem dois brokers e clientes UDP reais; um proxy TCP injeta
atraso bidirecional no enlace de metadados. O ensaio RTT compara o RTT
observado com o desconto aplicado ao TTL encaminhado. O ensaio de failover
publica continuamente, encerra o broker ativo e mede o intervalo entre o
último evento anterior e o primeiro posterior à reconexão. O teste local
não representa perda de host/rede física nem substitui a execução em
máquinas separadas descrita acima.

### Carga com encaminhamento, justiça e consumidor lento

Para exercitar os dois brokers, use o perfil do leilão com participantes em
ambos. `participante-3` a `participante-8` têm identidades próprias e
alternam a preferência A/B. Exemplo com quatro publicadores durante 60 s:

```sh
scripts/ensaio.sh preparar
LOG_FORMATO=text DURACAO=60s LANCES=20 REACOES=4 RAJADA=0 \
  docker compose --env-file scripts/ensaio.env --profile leilao up -d \
  leiloeiro espectador-a espectador-b participante-1 participante-2 participante-3 participante-4
sleep 65
scripts/ensaio.sh coletar carga-encaminhada
python scripts/graficos-carga.py ensaios/resultados/<diretorio-da-coleta>
```

O gerador produz `justica.csv`/`.svg`, `lacuna-failover.svg` e
`descartes.svg`. Justiça usa os eventos de lance vistos por um espectador e
calcula o índice de Jain (1 indica distribuição igual). Para avaliar o
efeito do segundo broker, compare com uma coleta equivalente após parar B e
usar só clientes que preferem A; mantenha taxa, duração e quantidade de
publicadores iguais. A lacuna de failover vem das linhas `lacuna de eventos`
coletadas pelo cenário UDP 8 (`scripts/ensaio.sh cenario 8 matar`, `travar`
ou `isolar`).

Para consumidor lento, repita a coleta com `ATRASO_CONSUMIDOR=50ms` (ou outro
atraso) e compare com `ATRASO_CONSUMIDOR=0`. O atraso é aplicado ao
processamento de cada evento no espectador e os resumos registram
`descartados_fila`, permitindo observar o descarte na fila limitada da
biblioteca.
