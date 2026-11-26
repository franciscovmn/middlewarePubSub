#!/usr/bin/env bash
# duas-maquinas.sh: ensaio de falha de host com o broker A num computador e
# o broker B com os clientes em outro (docs/ensaios.md, seção 5).
#
#   computador 1:  scripts/duas-maquinas.sh a <ip-do-computador-2>
#   computador 2:  scripts/duas-maquinas.sh clientes <ip-do-computador-1>[:porta]
#
# O papel "a" roda o broker A em primeiro plano, com o registro em
# ensaios/host/a.log. O papel "clientes" roda o broker B, o leilão (leiloeiro,
# participante-1 e espectador-a preferindo o A; participante-2 e espectador-b
# preferindo o B) e a sonda-a. Ele pede Enter logo antes da injeção, Enter
# logo depois, e Enter de novo para encerrar e coletar os registros em
# ensaios/resultados/<data>-host/.
#
# Variáveis: PORTA (comandos, padrão 7000), PORTA_UDP (eventos, padrão 7001)
# e PORTA_PAR (canal entre brokers, padrão 7002) do broker local; UDP_BASE
# (padrão 7301), primeira das portas UDP fixas dos clientes, que precisam
# estar liberadas no firewall do computador 2.
set -euo pipefail
cd "$(dirname "$0")/.."

PORTA=${PORTA:-7000}
PORTA_UDP=${PORTA_UDP:-7001}
PORTA_PAR=${PORTA_PAR:-7002}
UDP_BASE=${UDP_BASE:-7301}
CRED=config/credentials.ensaio.json
TOKEN_PAR=${TOKEN_PAR:-ensaio-par}

uso() { sed -n '2,19p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }
info() { printf 'host: %s\n' "$*" >&2; }

[ -x bin/broker ] && [ -x bin/leilao ] && [ -x bin/sonda ] || { info "rode make build antes"; exit 1; }
[ $# -ge 2 ] || uso

case $1 in
a)
	mkdir -p ensaios/host
	info "broker A em :$PORTA, par B em $2:$PORTA_PAR; registro em ensaios/host/a.log"
	exec bin/broker -id A -listen ":$PORTA" -udp-port "$PORTA_UDP" -credentials "$CRED" \
		-peer-id B -peer-admin -peer-addr "$2:$PORTA_PAR" -peer-token "$TOKEN_PAR" \
		2> >(tee ensaios/host/a.log >&2)
	;;
clientes)
	A=$2
	case $A in *:*) ;; *) A="$A:7000" ;; esac
	B="127.0.0.1:$PORTA"
	DIR="ensaios/resultados/$(date -u +%Y%m%dT%H%M%SZ)-host"
	mkdir -p "$DIR"
	PIDS=""
	parar() {
		# shellcheck disable=SC2086
		[ -n "$PIDS" ] && kill $PIDS 2>/dev/null || true
		wait 2>/dev/null || true
	}
	trap parar EXIT

	bin/broker -id B -listen ":$PORTA" -udp-port "$PORTA_UDP" -credentials "$CRED" \
		-peer-id A -peer-listen ":$PORTA_PAR" -peer-token "$TOKEN_PAR" 2>"$DIR/b.log" &
	PIDS="$PIDS $!"
	info "broker B iniciado; esperando o A conectar"
	until grep -q 'enlace: ativo' "$DIR/b.log"; do sleep 0.5; done

	leilao() {
		local papel=$1 id=$2 brokers=$3 porta=$4
		shift 4
		bin/leilao "$papel" -brokers "$brokers" -id "$id" -token "ensaio-$id" -udp ":$porta" \
			-resumo 2s "$@" 2>"$DIR/$id.log" &
		PIDS="$PIDS $!"
	}
	leilao leiloeiro leiloeiro "$A,$B" $((UDP_BASE)) -criar
	until grep -q 'msg=publicações' "$DIR/leiloeiro.log" 2>/dev/null; do sleep 0.5; done
	leilao espectador espectador-a "$A,$B" $((UDP_BASE + 1))
	leilao espectador espectador-b "$B,$A" $((UDP_BASE + 2))
	leilao participante participante-1 "$A,$B" $((UDP_BASE + 3))
	leilao participante participante-2 "$B,$A" $((UDP_BASE + 4))
	bin/sonda observar -brokers "$A,$B" -id sonda-a -token ensaio-sonda-a \
		-topicos sala-1/lances,sala-1/reacoes,sala-1/placar=estado 2>"$DIR/sonda-a.log" &
	PIDS="$PIDS $!"

	sleep 10
	if ! grep -q 'fluxo=participante-1/lances recebidos=[1-9]' "$DIR/espectador-b.log"; then
		info "o espectador-b não recebe o participante-1 pelo A: confira o firewall (UDP $PORTA_UDP e $UDP_BASE a $((UDP_BASE + 4)))"
	fi
	info "leilão rodando nos dois brokers"
	printf 'host: descreva a falha (ex.: energia cortada no computador 1) e tecle Enter logo antes de injetá-la: ' >&2
	read -r falha
	bin/sonda marcar -texto "injeção: $falha" 2>>"$DIR/marcas.log"
	printf 'host: injete a falha no computador 1 e tecle Enter assim que ela estiver feita: ' >&2
	read -r _
	bin/sonda marcar -texto "injeção concluída" 2>>"$DIR/marcas.log"
	printf 'host: aguarde ao menos 15 s e tecle Enter para encerrar e coletar: ' >&2
	read -r _
	bin/sonda marcar -texto "fim do ensaio" 2>>"$DIR/marcas.log"
	parar
	trap - EXIT
	scripts/linha-do-tempo.sh "$DIR" || true
	info "copie ensaios/host/a.log do computador 1 para $DIR/a.log e rode"
	info "  scripts/linha-do-tempo.sh $DIR"
	info "para incluir o broker A na linha do tempo"
	;;
*) uso ;;
esac
