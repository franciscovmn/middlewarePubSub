# Decisões de implementação

Decisões tomadas durante a implementação que o documento da Etapa 1 não
define. Cada item traz a justificativa e a alternativa descartada.

## Organização do código

**D1. Dependência do Protobuf com código gerado versionado.**
O módulo usa `google.golang.org/protobuf` e o código gerado em `internal/pb`
fica no repositório; `make proto` o recria.
Justificativa: quem só compila ou testa não precisa instalar o `protoc`.
Descartada: gerar o código no build, o que exigiria `protoc` e
`protoc-gen-go` em todas as máquinas.

**D2. Versão mínima do Go 1.23 no `go.mod`.**
Justificativa: os recursos usados existem desde essa versão, e nenhum
integrante é obrigado a atualizar o compilador.
Descartada: declarar a versão instalada localmente (1.27), que forçaria a
troca de toolchain nas outras máquinas.

**D3. Pacotes de apoio fora dos cinco módulos.**
`internal/pb` (código gerado), `internal/wire` (enquadramento),
`internal/contract` (interfaces entre os planos) e `internal/protoconv`
(conversões).
Justificativa: `broker` e `peer` dependem das mesmas interfaces; colocá-las em
um deles criaria import circular.
Descartada: definir as interfaces dentro de `broker`.

**D4. Identificadores em inglês e comentários em português.**
Justificativa: segue a convenção da linguagem e da biblioteca padrão.
Descartada: identificadores em português, que misturariam idiomas nas
chamadas à biblioteca padrão.

## Canal TCP de comandos

**D5. Prefixo de tamanho com 4 bytes big-endian.**
Justificativa: tamanho fixo, simples de ler, inspecionar e testar.
Descartada: varint (`protodelim`), de tamanho variável.

**D6. Teto de quadro configurável: 64 KiB no canal do cliente e 1 MiB no
canal entre brokers.**
O tamanho é verificado antes de alocar memória para o conteúdo.
Justificativa: um prefixo forjado não consegue reservar memória arbitrária;
1 MiB comporta o snapshot do catálogo.
Descartada: sem teto.

**D7. Mensagem `Envelope` com `oneof` e `request_id`.**
Mensagens enviadas pelo broker por iniciativa própria (resultado de
admissão, heartbeat, aviso de inscrição encerrada) usam `request_id` zero.
Justificativa: um único fluxo TCP carrega pedidos, respostas e avisos, e a
biblioteca casa cada resposta com seu pedido.
Descartada: um byte de tipo antes de cada mensagem, que repetiria o que o
`oneof` já faz.

**D8. Mensagens além da lista do documento.**
`PublishResult` (resultado de admissão), `Heartbeat` e `SubscriptionEnded`.
Justificativa: o resultado de admissão vai pelo TCP (seção 5.1), a falha é
detectada pela ausência de heartbeat (seção 8), e o cliente precisa saber
quando uma inscrição deixou de existir.
Descartada: detectar o fim da inscrição pela ausência de eventos, que não
distingue tópico apagado de tópico sem publicações.

**D9. `PublishResult` identificado por tópico, geração e sequência.**
Justificativa: a sequência é por publicador e tópico (seção 6.3); só a
sequência seria ambígua entre tópicos.
Descartada: identificar apenas pela sequência.

**D10. `HelloReply` informa a porta UDP do broker e o intervalo de
heartbeat.**
Justificativa: permite dois brokers na mesma máquina e ajustar o intervalo
sem mudar a biblioteca.
Descartada: porta 7001 fixa na biblioteca.

**D11. Violação de protocolo encerra a conexão.**
Primeira mensagem que não é Hello, segundo Hello, resposta enviada pelo
cliente ou corpo desconhecido.
Justificativa: o estado da conversa fica incerto e não há como recuperá-lo.
Descartada: responder erro e continuar.

**D12. Escrita com prazo; estouro encerra a conexão.**
O broker usa 1 s como prazo de escrita no canal do cliente.
Justificativa: uma escrita interrompida deixa um quadro parcial no fluxo; o
prazo também aplica a regra do consumidor que deixa de ler o canal de
controle por 1 s (seção 7.3).
Descartada: escrita sem prazo, em que um cliente parado bloquearia a
goroutine do broker.

## Autenticação e sessão

**D13. Credenciais estáticas em arquivo JSON.**
Cada entrada tem identidade, token e marca de administrador. O token é
comparado em tempo constante.
Justificativa: suficiente para o escopo e fácil de configurar nos ensaios.
Descartada: serviço externo de autenticação.

**D14. Sessão `uint64` gerada pela biblioteca e mantida na reconexão.**
Justificativa: a sequência por publicador continua válida ao trocar de
broker.
Descartada: sessão gerada pelo broker, que mudaria a cada reconexão.

**D15. Endereço UDP do cliente formado pelo IP de origem da conexão TCP e
pela porta informada no Hello.**
Justificativa: o cliente não consegue direcionar eventos para o endereço de
terceiros.
Descartada: o cliente informar o endereço UDP completo.

**D16. Sessão amarrada à identidade e ao endereço UDP do Hello.**
Hello que reutiliza sessão ligada a outra identidade é recusado. A mesma
identidade substitui o vínculo anterior: o broker encerra a conexão antiga
e espera a limpeza dela antes de responder.
Justificativa: impede o uso de sessão alheia; a limpeza atrasada da conexão
antiga não apaga as inscrições da nova.
Descartadas: aceitar qualquer sessão (permitiria sequestro) e recusar também
a mesma identidade (o cliente que reconecta antes de o broker notar a queda
ficaria bloqueado por até 5 s).

**D17. CreateTopic e DeleteTopic liberados a qualquer identidade
autenticada.**
A opção `RestrictTopicAdmin`, desligada por padrão, limita essas operações a
administradores. GetStats é sempre restrito, como define o documento.
Justificativa: o documento só restringe GetStats.
Descartada: restringir sempre, sem opção.

## Tópicos

**D18. Identificador e geração `uint32`; sessão `uint64`.**
Justificativa: cabem no cabeçalho UDP de 34 bytes. As larguras são
combinadas com o formato do cabeçalho.
Descartada: identificadores de 64 bits, que aumentariam o cabeçalho.

**D19. Identificador fixo por nome; a geração aumenta a cada recriação.**
Nomes apagados permanecem no catálogo com a última geração.
Justificativa: dá sentido a "geração antiga" no TOPIC_NOT_FOUND e impede que
a geração volte a 1 após apagar e recriar.
Descartada: novo identificador a cada recriação, em que a geração perderia a
função.

**D20. Nome com 1 a 128 bytes entre `a-z`, `0-9`, `_`, `-` e `/`.**
Justificativa: cobre nomes como `sala-1/lances` e evita problemas de
normalização e comparação de texto.
Descartada: qualquer texto UTF-8.

**D21. Validade máxima opcional na criação do tópico.**
Zero usa o padrão do tipo (500 ms ou 2 s); o limite é o que cabe em `uint32`
milissegundos.
Justificativa: ensaios podem ajustar a validade sem mudar o código.
Descartada: validade fixa por tipo.

**D22. Subscribe e Unsubscribe informam a geração; zero aceita a atual.**
Justificativa: na restauração após reconexão, a inscrição não pode passar
para um tópico recriado com o mesmo nome.
Descartada: inscrição só pelo nome.

## Prazos e limites

**D23. Heartbeat entre cliente e broker a cada 1 s, nos dois sentidos.**
Cada lado encerra a conexão após 5 s sem receber nada; qualquer mensagem
conta. O Hello também precisa chegar em até 5 s.
Justificativa: o intervalo bem menor que o limite tolera perdas pontuais;
o heartbeat do broker permite ao cliente detectar um broker travado.
Descartada: heartbeat só do cliente.

**D24. Prazo do resultado de admissão de 1 s, configurável.**
`Wait` devolve UNKNOWN sem erro quando o prazo da biblioteca se esgota e só
devolve erro se o contexto terminar.
Justificativa: UNKNOWN é um resultado previsto (seção 5.2), não uma falha.
Descartada: usar a validade do evento como prazo, que variaria por tópico.

**D25. Mais de 4.096 publicações pendentes falham na hora, sem consumir
sequência.**
Justificativa: a publicação continua assíncrona, e o assinante não vê
lacuna de sequência causada por uma recusa local.
Descartada: bloquear `Publish` até abrir vaga.

**D26. Fila de saída de 4.096 mensagens por conexão para o que o broker
envia por iniciativa própria.**
Com a fila cheia, o resultado é descartado e o cliente conclui UNKNOWN pelo
prazo.
Justificativa: o plano de dados nunca bloqueia esperando o TCP; 4.096 é o
máximo de pendentes de um cliente.
Descartada: bloquear o plano de dados até a escrita.

## Biblioteca cliente

**D27. Eventos entregues por canal, com a fila seguindo a política do
tópico.**
256 eventos descartando o mais antigo no efêmero; um evento, com a nova
versão substituindo a pendente, no de estado. `Err()` informa por que a
inscrição terminou.
Justificativa: segue o uso de canais previsto no plano de implementação.
Descartada: função de retorno chamada a cada evento.

**D28. `Publish` consulta o tópico no broker quando não está em cache.**
A consulta usa o prazo de publicação. Um resultado TOPIC_NOT_FOUND tira o
tópico do cache.
Justificativa: a aplicação publica pelo nome sem chamadas preparatórias, e
uma recriação do tópico é percebida na publicação seguinte.
Descartada: exigir LookupTopic antes da primeira publicação.

**D29. Uma inscrição por tópico em cada cliente.**
Assinar de novo devolve `ErrAlreadySubscribed`; Unsubscribe de algo não
inscrito responde OK.
Justificativa: duas inscrições no mesmo tópico duplicariam a entrega no
mesmo processo.
Descartada: várias inscrições por tópico.

**D30. `Dial` não tenta o outro broker quando as credenciais são
recusadas.**
Justificativa: as credenciais valem para os dois brokers; tentar o outro só
adiaria o erro.
Descartada: percorrer toda a lista em qualquer erro.

**D31. Interface `DataPlane` para o lado UDP do cliente.**
Deduplicação por sequência e descarte de eventos expirados ficam nessa
implementação, antes da entrega à inscrição.
Justificativa: separa o canal de controle do transporte de eventos.
Descartada: o canal de controle tratar datagramas.

**D32. Restauração recusada por geração antiga fecha a inscrição com erro
explícito.**
Justificativa: a aplicação precisa saber que o tópico que assinava deixou de
existir.
Descartada: restaurar silenciosamente na nova geração.

## Contratos com o plano de dados

**D33. Interfaces de leitura para o plano de dados; escrita nos tipos
concretos.**
Justificativa: só o plano de controle altera sessões, catálogo e inscrições.
Descartada: interfaces com métodos de escrita expostos aos dois planos.

**D34. Canal `Done()` em cada inscrição.**
Fechado em Unsubscribe, queda da conexão, encerramento por consumidor lento
ou exclusão do tópico.
Justificativa: o plano de dados libera a fila da inscrição sem varrer a
tabela.
Descartada: varredura periódica.

**D35. Idade única para a visão de interesse do par.**
Qualquer mensagem do par renova a visão inteira; a queda do enlace a
descarta.
Justificativa: a idade da visão já reflete o estado do enlace, e a admissão
decide NO_SUBSCRIBERS ou UNAVAILABLE consultando só a visão.
Descartada: idade por tópico, que exigiria mensagens por tópico para
renová-la.

**D36. `LinkStatus` com RTT.**
Justificativa: o encaminhamento desconta RTT/2 da validade (seção 6.3).
Descartada: estado do enlace só com vivo ou fora.

**D37. Sem par configurado, a visão do par é sempre recente e vazia.**
Justificativa: com um único broker não há assinantes remotos, então
NO_SUBSCRIBERS pode ser confirmado.
Descartada: tratar a ausência de par como enlace fora, o que faria toda
publicação sem assinantes locais receber UNAVAILABLE.

**D38. Estatísticas e cota de comandos como contratos.**
`StatsSource` alimenta o GetStats e `CommandLimiter` aplica a cota de 20
comandos/s; os stubs não restringem nada.
Justificativa: os dois pertencem ao plano de dados e ao diagnóstico, mas são
consultados pelo canal de controle.
Descartada: implementar contadores e cotas dentro do broker.

## Canal entre brokers

**D39. Papéis fixos: o A disca, o B escuta.**
Cada lado confere o identificador esperado do par no `PeerHello`; um token
compartilhado é opcional.
Justificativa: regra determinística, sem desempate entre duas discagens
simultâneas.
Descartada: os dois discarem e um desempate escolher a conexão.

**D40. Conexão duplicada recusada; o A tenta de novo com recuo
progressivo.**
O recuo começa em 100 ms, dobra até 5 s e volta ao início após conectar.
Justificativa: enquanto o B ainda não percebeu a queda da conexão antiga, a
nova é recusada e o A espera sem sobrecarregar o par.
Descartada: a nova conexão substituir a antiga, que poderia oscilar entre
as duas.

**D41. Ordem de entrada no enlace.**
O A envia o snapshot do catálogo e espera a confirmação; depois cada lado
envia o snapshot de interesse. Só então o enlace conta como vivo para
alterações do catálogo e novas inscrições.
Justificativa: nenhuma alteração incremental chega antes do estado
completo.
Descartada: liberar o enlace logo após o Hello.

**D42. Heartbeat de 250 ms nos dois sentidos, par fora após 5 s sem
receber nada.**
Cada lado envia `Ping` e responde `Pong`; o RTT do `LinkStatus` é a última
amostra.
Justificativa: a visão de interesse precisa ser renovada bem antes de 1 s;
a detecção de falha segue os 5 s do documento.
Descartada: média móvel do RTT, que fica para avaliação nos ensaios.

**D43. Alteração sem ACK em 2 s retorna UNAVAILABLE, não é aplicada no A e
derruba o enlace.**
Justificativa: o B pode ter aplicado a alteração; o snapshot da reconexão
desfaz a diferença. Sem derrubar o enlace, as versões dos dois lados
divergiriam em silêncio.
Descartada: manter o enlace e seguir com a próxima versão.

**D44. `operation_id` aleatório de 128 bits.**
A alteração repassada pelo B mantém o identificador gerado no B.
Justificativa: um contador recomeçaria do zero se o A reiniciasse e
colidiria com o histórico guardado no B.
Descartada: contador por broker.

**D45. O B aplica uma atualização só se a versão for a dele mais um.**
Versão já aplicada ou `operation_id` já visto é confirmado sem reaplicar;
um salto de versão é recusado e o A derruba o enlace, forçando novo
snapshot. O B guarda os últimos 1.024 identificadores, descartados a cada
snapshot.
Justificativa: a ordem do TCP garante a sequência dentro de uma conexão; o
snapshot ressincroniza entre conexões.
Descartada: aplicar fora de ordem.

**D46. Alterações do catálogo serializadas no A.**
O snapshot enviado numa reconexão também espera a alteração em curso.
Justificativa: a versão planejada é sempre a seguinte à atual, e o snapshot
nunca fica entre o ACK do B e a aplicação no A.
Descartada: alterações concorrentes com versões reservadas.

**D47. O B repassa CreateTopic e DeleteTopic ao A e só responde ao cliente
depois da resposta do A.**
O A replica a alteração ao B antes de responder, e a resposta segue a
atualização no mesmo fluxo; ao responder, o B já aplicou. O prazo do
repasse é o dobro do prazo de ACK. Com o enlace fora, o B responde
UNAVAILABLE.
Justificativa: o cliente vê o tópico no próprio broker assim que recebe a
confirmação.
Descartada: o cliente abrir uma conexão com o A só para administração.

**D48. O snapshot do catálogo leva os nomes apagados com a última geração.**
O B remove os tópicos ausentes do snapshot, ou presentes com outra geração,
e encerra as inscrições deles com `SubscriptionEnded`.
Justificativa: a geração continua crescendo no B após uma reconexão.
Descartada: snapshot só com os tópicos ativos.

**D49. Interesse anunciado só quando o tópico passa a ter ou deixa de ter
assinantes locais.**
Subscribe num tópico que já tem assinantes locais confirma sem ida e volta,
mas exige o enlace vivo. Snapshot inicial e atualizações numeradas, com
snapshot completo a cada reconexão.
Justificativa: o par só precisa saber se há assinantes, não quantos.
Descartada: anunciar cada inscrição.

**D50. Unsubscribe remove a inscrição local mesmo sem o ACK do par.**
Justificativa: sem ACK o enlace cai, o par descarta a visão e recebe o
snapshot completo na volta, então não fica interesse obsoleto.
Descartada: responder UNAVAILABLE e manter uma inscrição que o cliente quer
encerrar.

**D51. A queda da conexão de um cliente retira o interesse em segundo
plano.**
Justificativa: a limpeza da conexão não espera o par.
Descartada: retirar o interesse de forma síncrona na limpeza.

**D52. A visão de interesse só é renovada depois do snapshot de interesse
da sessão.**
Justificativa: uma visão descartada não pode voltar a parecer recente e
vazia antes de o par informar o estado real.
Descartada: renovar a visão a qualquer mensagem, desde a conexão.

**D53. A resposta ao Subscribe é escrita com o catálogo travado para
leitura.**
Justificativa: um aviso de exclusão do mesmo tópico não chega ao cliente
antes da confirmação da inscrição. Vale também sem par.
Descartada: responder depois de liberar o catálogo, o que deixava uma
inscrição sem aviso de encerramento.

**D54. O A que sobe sem catálogo adota o catálogo do B.**
Na primeira conexão, se a versão do catálogo local é zero, o A pede o
snapshot ao B (`CatalogSnapshotRequest`) e o adota, com os nomes apagados e
a última geração de cada um, antes de enviar o próprio snapshot. Até lá o
enlace não fica vivo e as alterações administrativas recebem UNAVAILABLE.
A adoção acontece uma vez por execução; nas reconexões seguintes o A volta
a enviar o próprio catálogo. Tópicos, gerações e inscrições do B
sobrevivem ao reinício do A, e recriar um nome continua a geração.
Justificativa: o catálogo fica só em memória; sem a adoção, o B espelharia
o catálogo vazio do A reiniciado e encerraria todas as inscrições.
Descartada: o B espelhar o catálogo vazio do A; persistir o catálogo em
disco.
Limitação: se os dois reiniciarem antes de se sincronizarem, o catálogo se
perde.

**D55. Proxy TCP de testes em `internal/testproxy`.**
Corta conexões, atrasa e descarta tráfego sem fechar as conexões.
Justificativa: simula partições e atraso com dois brokers na mesma máquina.
Descartada: regras de firewall do sistema operacional, que exigem
privilégios.

## Ajustes antes da reconexão do cliente

**D56. Cada broker informa a própria porta UDP no handshake do canal entre
brokers.**
O IP vem da conexão TCP do enlace; `-peer-udp-addr` sobrescreve o endereço
inteiro. O endereço fica em `LinkStatus.PeerUDP`, exposto por `Contracts()`,
e mantém o último valor conhecido após a queda do enlace.
Justificativa: o plano de dados encaminha eventos ao par sem configuração
duplicada; a sobrescrita cobre NAT e contêineres com portas mapeadas.
Descartada: configurar o endereço UDP do par sempre à mão.

**D57. Broker com par responde UNAVAILABLE a LookupTopic e Subscribe até a
primeira sincronização do catálogo.**
No A, a sincronização é a adoção ou o envio do snapshot; no B, o
recebimento do snapshot.
Justificativa: um broker recém iniciado tem o catálogo vazio, e responder
TOPIC_NOT_FOUND levaria o cliente a encerrar como apagada uma inscrição de
tópico que existe.
Descartada: responder pelo catálogo vazio.

**D58. A restauração de inscrições é aceita com o enlace entre brokers
fora.**
O Subscribe leva `restore = true` na restauração. O broker registra a
inscrição localmente e o interesse segue ao par no snapshot de interesse
quando o enlace voltar. Inscrições novas continuam recebendo UNAVAILABLE
com o enlace fora.
Justificativa: no failover típico o A caiu e o enlace está fora; recusar a
restauração deixaria os assinantes parados justamente no cenário de falha.
Sem enlace não há encaminhamento entre brokers, e a visão do par é
reenviada por completo na volta, então não há interesse obsoleto.
Descartada: tentar a restauração até o enlace voltar; fechar a inscrição
com erro.

## Reconexão do cliente

**D59. Detecção e ordem de tentativas.**
A falha é a perda do TCP ou `HeartbeatTimeout` (5 s) sem receber nada do
broker. A biblioteca tenta os brokers da lista a partir do seguinte ao que
falhou, em ordem circular. A primeira tentativa é imediata; as seguintes
esperam um recuo que começa em 100 ms e dobra até 5 s. Cada tentativa tem o
prazo de `HeartbeatTimeout`. Por padrão tenta até `Close`;
`ReconnectTimeout` limita o tempo total e, esgotado, encerra o cliente com
`ErrConnectionLost`. Recusa de credencial ou de sessão encerra o cliente
sem novas tentativas.
Justificativa: com dois brokers o outro é tentado primeiro, e o recuo evita
martelar brokers fora do ar.
Descartada: desistir após um número fixo de tentativas por padrão.

**D60. Sem retorno automático.**
O cliente só troca de broker depois de uma falha; quando o broker original
volta, o cliente continua no atual.
Justificativa: evita uma segunda interrupção sem necessidade e mantém a
carga onde está.
Descartada: voltar ao broker preferido quando ele reaparecer.

**D61. Restauração com a mesma sessão e a geração conhecida.**
O novo Hello usa a mesma sessão. Cada inscrição ativa é reenviada com a
geração conhecida e `restore = true` (D58). TOPIC_NOT_FOUND encerra a
inscrição; uma consulta pelo nome distingue `ErrTopicDeleted` de
`ErrTopicRecreated`. UNAVAILABLE (catálogo não sincronizado, D57) ou queda
do canal encerram a tentativa, e a restauração recomeça no broker seguinte.
Outros erros encerram a inscrição com o erro correspondente.
Justificativa: a geração impede que uma inscrição passe a receber eventos
de outra versão do tópico sem a aplicação saber.
Descartada: reinscrever pelo nome na geração atual.

**D62. Os canais de `Events()` permanecem abertos durante a troca.**
`Err()` continua nil. Os eventos do novo broker chegam pelo mesmo canal.
Justificativa: a aplicação não precisa refazer o laço de leitura a cada
falha.
Descartada: fechar as inscrições e exigir nova inscrição.

**D63. Política de UNKNOWN: sem retransmissão automática.**
As publicações pendentes viram UNKNOWN no momento da detecção da falha, sem
esperar o prazo de `PublishTimeout`. A biblioteca nunca reenvia uma
publicação; a decisão fica com a aplicação.
Riscos:
- UNKNOWN não significa perda. O broker antigo pode ter admitido e
  entregado o evento antes de cair.
- Se a aplicação republicar, o evento ganha nova sequência, e o novo broker
  não tem como reconhecê-lo como repetido. Os assinantes podem receber o
  mesmo conteúdo duas vezes.
- Se a aplicação não republicar, o evento pode ter se perdido.

Para tópicos de estado, republicar é seguro, pois a versão mais nova
substitui a anterior. Para tópicos efêmeros, como lances, a aplicação que
republica deve tolerar duplicatas, por exemplo com um identificador no
próprio conteúdo.
Justificativa: retransmitir sem saber o que foi admitido transformaria
toda falha em duplicação silenciosa.
Descartada: reenviar as pendentes ao novo broker com a mesma sequência, o
que exigiria deduplicação compartilhada entre os brokers.

**D64. Publish e comandos falham na hora durante a reconexão.**
`Publish`, `Subscribe`, `CreateTopic`, `DeleteTopic`, `LookupTopic` e
`Stats` retornam `ErrReconnecting` sem esperar. Um comando em curso quando
o canal cai retorna `ErrConnectionLost`, com efeito desconhecido no broker.
Justificativa: a aplicação de tempo real decide se espera ou descarta; um
Publish bloqueado atrasaria eventos que já estariam vencidos.
Descartada: enfileirar os comandos até a reconexão.

**D65. A sequência por tópico continua após a troca.**
Justificativa: a sessão é a mesma, e a ordem por publicador se mantém para
os assinantes.
Descartada: reiniciar a sequência no novo broker.

**D66. Casos de borda da restauração.**
- Uma inscrição confirmada por um canal que caiu antes do registro local
  retorna `ErrReconnecting` e não entra na restauração.
- `Unsubscribe` durante a reconexão termina a inscrição só localmente,
  pois a do broker antigo caiu com a conexão.
- Uma inscrição restaurada e cancelada durante a tentativa é retirada do
  novo broker logo após a troca.

Justificativa: o broker não fica com inscrições que a aplicação não tem.
Descartada: recusar `Unsubscribe` durante a reconexão.

**D67. O cache de tópicos é descartado na detecção da falha.**
Justificativa: o catálogo do próximo broker passa a ser a referência; a
primeira publicação em cada tópico refaz a consulta.
Descartada: manter o cache e depender do resultado TOPIC_NOT_FOUND.

**D68. Instrumentação da lacuna.**
A biblioteca registra em log (`log/slog`) a detecção da falha e a
restauração. O registro traz:
- a lacuna, entre a detecção da falha e o fim da restauração das
  inscrições;
- o número de tentativas;
- as inscrições restauradas e as encerradas.

`Config.OnReconnect` recebe os mesmos dados em `Reconnect`, com `Gap()`.
`Config.Logger` escolhe o destino, com `slog.Default()` por padrão.
Justificativa: a medição fica disponível tanto em log quanto para os
ensaios automatizados.
Descartada: só log em texto, que exigiria interpretar a saída nos ensaios.
A lacuna de eventos, do último antes da falha ao primeiro depois dela,
fica para a integração com o plano de dados.

**D69. O limite de silêncio do cliente cobre ao menos três heartbeats do
broker.**
Ao receber o HelloReply, o cliente compara o intervalo anunciado com o
próprio limite. Se o limite for menor que três intervalos, o cliente o
eleva para três intervalos naquele canal e registra um aviso. O cliente
envia heartbeats no intervalo anunciado.
Justificativa: um limite menor faria o cliente trocar de broker sem falha
real. Três intervalos toleram a perda ou o atraso de dois heartbeats.
Descartada: recusar a conexão; manter o limite configurado e só avisar.

## Ensaios de falha

**D70. Registros estruturados com horário UTC em milissegundos.**
Broker, enlace, biblioteca e sonda registram com `log/slog`, em texto ou
JSON. Cada evento de falha, enlace, catálogo, sessão e restauração tem uma
mensagem própria, com a causa. As tentativas de reconexão do enlace saem
em nível debug.
Justificativa: a lacuna do failover é medida comparando registros de
processos diferentes, e o fuso UTC evita ambiguidade entre máquinas.
Descartada: registro em texto livre com o horário local.

**D71. Sonda de ensaio só com o canal de controle (`cmd/sonda`).**
Ela mantém inscrições, registra cada failover com a lacuna, consulta o
canal de comandos periodicamente e executa comandos avulsos, incluindo
marcas de horário.
Justificativa: permite ensaiar a seção 8 sem depender do plano de dados
UDP.
Descartada: esperar o aplicativo do leilão para começar os ensaios.

**D72. Falhas de rede injetadas com `iptables` e `tc` dentro dos
contêineres.**
Os serviços têm `NET_ADMIN`; o proxy de testes fica só nos testes
automatizados.
Justificativa: descarta pacotes de verdade, mantém o endereço UDP do par
aprendido pelo IP real e serve aos ensaios de carga com perda e atraso de
datagramas.
Descartada: um proxy TCP entre os contêineres, que não cobre UDP e exigiria
sobrescrever o endereço do par.

**D73. Variantes de falha.**
- Processo: morto (`SIGKILL`), parado (`SIGTERM`), travado (`pause`) e
  isolado da rede (`network disconnect`).
- Corte silencioso: descarte na entrada, nos dois lados.
- Corte explícito: RST nos dois sentidos, aplicado num lado, com os próprios
  RST liberados para que uma regra não descarte o RST da outra.

Justificativa: separa a detecção por encerramento do TCP da detecção por
silêncio, que é o caso da falha de host.
Descartada: uma única forma de falha por cenário.

**D74. Medidas relativas a uma marca de injeção registrada pela sonda.**
A marca é gravada logo antes do comando de injeção, com o mesmo relógio e
o mesmo formato dos registros. As medidas por sonda são três:
- injeção até a detecção;
- injeção até a restauração;
- lacuna da biblioteca.

Justificativa: a lacuna da biblioteca começa na detecção e não mostra os
5 s de silêncio das falhas silenciosas; a interrupção vista pela aplicação
começa na falha.
Descartada: usar só a lacuna registrada pela biblioteca.

**D75. Roteiros com verificação automática do resultado esperado.**
Cada resultado esperado é registrado como `ok` ou `divergente`, sem
interromper o cenário, e os registros são sempre guardados.
Justificativa: os ensaios se repetem a cada mudança, e uma divergência
precisa dos registros para ser analisada.
Descartada: conferir os registros à mão.

## Aplicação do leilão

**D76. Aplicação com três papéis em processos separados (`cmd/leilao`).**
Leiloeiro, participante e espectador, como na seção 2. Cada processo tem a
própria identidade, a própria sessão e o próprio plano de dados UDP.
Justificativa: cotas por identidade, troca de broker e encaminhamento entre
brokers só aparecem com clientes independentes, em brokers diferentes.
Descartada: um processo único com todos os papéis.

**D77. Mensagens em JSON com o publicador e o horário de envio.**
Todo evento leva `p` (identidade) e `t` (nanossegundos Unix), além dos
campos do tipo.
Justificativa: o espectador nomeia o fluxo e mede a latência sem conhecer o
tipo da mensagem, e o conteúdo fica legível nos registros. As mensagens têm
menos de 200 bytes, bem abaixo do limite de 1.024.
Descartada: formato binário próprio, cuja economia não pesa nesse tamanho.

**D78. Placar publicado em intervalo fixo (100 ms), e não a cada lance.**
Justificativa: no tópico de estado, cada publicação substitui a anterior.
A publicação periódica atualiza quem acabou de entrar ou perdeu eventos,
mantém a taxa do leiloeiro constante e dentro da cota, e dá um fluxo
regular para medir a lacuna.
Descartada: publicar a cada lance novo, o que deixaria a taxa do leiloeiro
depender dos participantes.

**D79. O participante lança acima do maior valor que conhece.**
Esse valor vem do placar ou do próprio último lance, com um incremento
aleatório. O leiloeiro decide o maior lance.
Justificativa: lances crescentes sem coordenação entre os participantes,
como num leilão real com atraso de informação.
Descartada: valores aleatórios independentes do placar.

**D80. Lacuna de eventos detectada pela cadência de cada fluxo.**
- A cadência é o maior dos últimos 20 intervalos normais.
- Uma lacuna é um intervalo acima de 1,5 vez a cadência e com ao menos
  20 ms a mais; ela é registrada quando o evento seguinte chega.
- A medida do ensaio é a maior lacuna que cobre o intervalo entre as
  marcas de injeção.

Justificativa: a primeira versão media o maior intervalo depois da
injeção e confundia variação normal com falha. Com o processo morto, a
troca de broker cabe entre dois eventos, e o resultado correto é a
ausência de lacuna. O máximo, e não a média, evita que as rajadas de
reações reduzam a cadência.
Descartada: limiar fixo em milissegundos, que dependeria da taxa de cada
fluxo, e média móvel dos intervalos.

**D81. Leilão num perfil separado do compose.**
Justificativa: os cenários 1 a 7 continuam com o mesmo ambiente e
resultados comparáveis; o cenário 8 e os ensaios de carga sobem o perfil.
Descartada: subir o leilão em todos os cenários.

## Ajustes da biblioteca cliente

**D82. Conteúdo acima de 1.024 bytes é recusado na biblioteca com
INVALID_SIZE.**
`Publish` devolve a publicação já concluída com `StatusInvalidSize` e
sequência zero, sem enviar o datagrama e sem consumir sequência.
Justificativa: a seção 5.2 pede a rejeição com INVALID_SIZE, e o broker
não consegue responder a um datagrama que nem decodifica, o que levaria a
um UNKNOWN depois de 1 s. Como em D25, a recusa local não cria lacuna de
sequência para o assinante.
Descartada: devolver um erro de `Publish`, que não aparece na tabela de
resultados do documento.

**D83. Sem `DataPlane` configurado, `Dial` abre o UDP numa porta livre.**
O socket é encerrado com o cliente, ou na hora, se `Dial` falhar.
`MemoryDataPlane` continua disponível para testes sem rede.
Justificativa: a biblioteca abre "os canais TCP de controle e UDP de
eventos" (seção 4.2). Com o plano em memória como padrão, um cliente
configurado sem `DataPlane` conectava, mas não enviava nem recebia
eventos, sem nenhum erro.
Descartada: manter o plano em memória como padrão e exigir que a aplicação
abra o UDP.

## Falha dos dois brokers

**D84. Com os dois brokers fora, o serviço só volta quando os dois estão
de pé.**
Um broker que volta sozinho aceita o Hello, mas responde UNAVAILABLE a
LookupTopic, Subscribe e restaurações, porque o catálogo dele está vazio e
não sincronizado (D57). Também não cria nem apaga tópicos, porque as
alterações administrativas exigem o ACK do par (D43). Quando o segundo
volta, o A adota o catálogo do B (D54). Como não há persistência, os
tópicos precisam ser recriados, e as inscrições antigas terminam com
`ErrTopicDeleted`.
Justificativa: a tabela da seção 8 diz "serviço indisponível até ao menos
um broker voltar". Atender isso ao pé da letra exigiria que um broker
sozinho aceitasse alterações administrativas sem o par. Isso contraria a
regra da seção 6.1, de que o A só aplica uma alteração depois do ACK do B.
Também criaria catálogos divergentes, que o snapshot da reconexão
resolveria apagando tópicos e encerrando inscrições. O desvio vale só para
a falha simultânea dos dois brokers; com um deles de pé, a seção 8 é
seguida como escrita.
Descartada: permitir que o broker que volta sozinho crie tópicos e
responda pelo catálogo sem o par.

## Distribuição de eventos

**D85. Filas por inscrição ficam na biblioteca cliente; o broker envia os
datagramas diretamente.**
O broker não mantém uma fila individual para cada inscrição. Depois da
admissão, ele agenda o evento nas filas globais de distribuição e envia o
datagrama aos endereços UDP das inscrições ativas. Ao receber, a biblioteca
cliente coloca o evento na fila daquela inscrição: 256 eventos para tópico
efêmero, descartando o mais antigo quando cheia, e um evento para tópico de
estado, substituindo a versão pendente. `Subscription.Dropped()` expõe os
descartes observados localmente.

Justificativa: o isolamento do consumidor lento ocorre no cliente, onde a
capacidade e a política são por inscrição; um consumidor não bloqueia o
envio do broker nem ocupa uma fila de saída dedicada nele. O custo é que
descartes por fila cheia são locais e não fazem o broker repetir o evento.
Esta é uma divergência deliberada da seção 4.2, que descreve filas por
inscrição no broker. O comportamento observável de capacidade e descarte é
preservado, mas a localização da fila difere.
Descartada: manter no broker uma fila de saída por inscrição, que duplicaria
bufferização e associaria o progresso do consumidor ao processo central.

**D86. O encaminhamento entre brokers compartilha o socket e a porta UDP
de eventos (7001).**
O broker envia datagramas encaminhados pela mesma conexão UDP usada para
receber publicações e entregar eventos aos clientes. O destino do par é
aprendido no handshake do enlace (D56); ao receber um datagrama marcado
como encaminhado, o broker exige enlace ativo e confere que o endereço de
origem corresponde ao endereço UDP anunciado pelo par.

Justificativa: mantém uma única porta UDP por broker para o plano de dados,
simplificando publicação, encaminhamento e regras de firewall. A origem
validada pelo enlace impede que um cliente comum se passe pelo par. O
encaminhamento continua limitado a um salto, pela marca de encaminhamento.
Descartada: reservar uma segunda porta UDP só para o tráfego entre brokers.

**D87. Limite global de distribuição de 320 datagramas por segundo por
broker, com rajada de 320.**
O limite conta cada datagrama enviado, seja a um inscrito, seja ao par, e
é aplicado por um token bucket no despacho. O valor é o dobro da carga
medida no pior caso do leilão de exemplo: com o A fora, o B sozinho envia
162 datagramas por segundo, contados com `iptables` no contêiner. Em
operação normal, o A envia 107 e o B, 86. A taxa e a rajada são
configuráveis no broker (`-distribuicao-por-segundo`,
`-distribuicao-rajada`) e nos ensaios (`DISTRIBUICAO`,
`DISTRIBUICAO_RAJADA`).
Justificativa: a seção 7.2 pede limitar o volume de distribuição a cerca de
duas vezes a carga esperada. O primeiro valor, 60 por segundo, ficava
abaixo da própria carga do leilão: depois do failover, os lances e as
reações expiravam na fila, e o espectador recebia só o placar. Com 320, o
cenário 8 volta a ter latência média abaixo de 1 ms, e a rajada de um
segundo cobre as rajadas de reações sem espera.
Descartada: 60 por segundo, abaixo da carga medida; e liberar um evento
por intervalo fixo, sem rajada, que atrasava todos os eventos mesmo sem
carga.
