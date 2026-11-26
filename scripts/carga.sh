#!/usr/bin/env bash
# carga.sh <nome>: ensaio de carga com o leilão nos contêineres, sem as
# sondas. Sobe os brokers, o leiloeiro, os dois espectadores e os
# participantes, mede os datagramas UDP enviados por segundo e a CPU de
# cada broker numa janela no meio da execução, e guarda os registros em
# ensaios/resultados/<data>-carga-<nome>/.
#
# Variáveis (além das de scripts/ensaio.env, que podem ser sobrescritas no
# ambiente, como LANCES, REACOES, RAJADA, DISTRIBUICAO):
#   TOPOLOGIA      dois (padrão) ou um: só o A, sem par configurado
#   PARTICIPANTES  serviços participantes (padrão participante-1 a 4)
#   DURACAO        duração dos participantes (padrão 60s)
#   PESADO         participante com argumentos próprios, em PESADO_ARGS
#   LENTO          espectador com atraso por evento, em ATRASO_LENTO
#
#   scripts/carga.sh base
#   TOPOLOGIA=um scripts/carga.sh um-broker
#   LENTO=espectador-b ATRASO_LENTO=50ms scripts/carga.sh consumidor-lento
set -euo pipefail
cd "$(dirname "$0")/.."

NOME=${1:?uso: carga.sh <nome>}
ENSAIO_ENV=${ENSAIO_ENV:-scripts/ensaio.env}
RESULTADOS=${RESULTADOS:-ensaios/resultados}
TOPOLOGIA=${TOPOLOGIA:-dois}
PARTICIPANTES=${PARTICIPANTES:-participante-1 participante-2 participante-3 participante-4}
export DURACAO=${DURACAO:-60s}
JANELA=30

DIR="$RESULTADOS/$(date -u +%Y%m%dT%H%M%SZ)-carga-$NOME"
mkdir -p "$DIR"
info() { printf 'carga: %s\n' "$*" >&2; }

dc() { docker compose --env-file "$ENSAIO_ENV" --profile leilao "$@"; }

contar() { dc logs --no-color --no-log-prefix "$1" 2>/dev/null | grep -c -- "$2" || true; }

esperar() {
	local fim=$((SECONDS + 30))
	until [ "$(contar "$1" "$2")" -ge "$3" ]; do
		[ $SECONDS -lt $fim ] || { info "sem \"$2\" em $1"; exit 1; }
		sleep 0.2
	done
}

# A sobreposição do compose muda só o necessário: o A sem par na topologia
# de um broker, os argumentos do participante pesado e o atraso do
# espectador lento.
sobrepor() {
	dc config --format json | TOPOLOGIA=$TOPOLOGIA PESADO=${PESADO:-} PESADO_ARGS=${PESADO_ARGS:-} \
		LENTO=${LENTO:-} ATRASO_LENTO=${ATRASO_LENTO:-50ms} python3 -c '
import json, os, sys
svc = json.load(sys.stdin)["services"]
out = {}
if os.environ["TOPOLOGIA"] == "um":
    out["a"] = {"command": [x for x in svc["a"]["command"] if not x.startswith("-peer")]}
if os.environ["PESADO"]:
    p = os.environ["PESADO"]
    out[p] = {"command": svc[p]["command"] + os.environ["PESADO_ARGS"].split()}
if os.environ["LENTO"]:
    e = os.environ["LENTO"]
    cmd = [x for x in svc[e]["command"] if not x.startswith("-processamento")]
    out[e] = {"command": cmd + ["-processamento=" + os.environ["ATRASO_LENTO"]]}
json.dump({"services": out}, sys.stdout)
' >"$DIR/sobreposicao.json"
	export COMPOSE_FILE="docker-compose.yml:$DIR/sobreposicao.json"
}

datagramas() { dc exec -T "$1" iptables -L OUTPUT -v -x -n | awk '/udp spt:7001/ { print $1 }'; }

dc down --remove-orphans --timeout 3 >/dev/null 2>&1 || true
sobrepor
BROKERS="a b"
[ "$TOPOLOGIA" = um ] && BROKERS=a
# shellcheck disable=SC2086
dc up -d $BROKERS >/dev/null 2>&1
if [ "$TOPOLOGIA" = um ]; then
	esperar a 'broker iniciado' 1
else
	esperar a 'enlace: ativo' 1
fi
dc up -d --no-deps leiloeiro espectador-a espectador-b >/dev/null 2>&1
esperar leiloeiro 'msg="tópico ' 3
esperar espectador-a 'msg=inscrito' 3
esperar espectador-b 'msg=inscrito' 3
# shellcheck disable=SC2086
dc up -d --no-deps $PARTICIPANTES >/dev/null 2>&1
INICIO=$SECONDS
info "$NOME: $TOPOLOGIA broker(s), participantes: $PARTICIPANTES, duração $DURACAO"

sleep 10
for b in $BROKERS; do dc exec -T "$b" iptables -I OUTPUT -p udp --sport 7001; done
{
	echo "janela de ${JANELA}s, 10s após o início dos participantes"
	antes=""
	for b in $BROKERS; do antes="$antes $b=$(datagramas "$b")"; done
	cpu=""
	for i in 1 2 3; do
		sleep $((JANELA / 3))
		cpu="$cpu $(docker stats --no-stream --format '{{.Name}}={{.CPUPerc}}' $(dc ps -q $BROKERS) | tr '\n' ' ')"
	done
	for par in $antes; do
		b=${par%%=*}
		echo "broker $b: $(( ($(datagramas "$b") - ${par#*=}) / JANELA )) datagramas UDP/s enviados"
	done
	echo "CPU (3 amostras):$cpu"
} >"$DIR/carga.txt"

# Espera os participantes terminarem e os espectadores fecharem o resumo.
restante=$(( ${DURACAO%s} - (SECONDS - INICIO) + 4 ))
[ "$restante" -gt 0 ] && sleep "$restante"

for svc in $(dc ps -a --services); do
	dc logs --no-color --no-log-prefix "$svc" >"$DIR/$svc.log" 2>/dev/null || true
done
dc down --remove-orphans --timeout 3 >/dev/null 2>&1 || true
scripts/linha-do-tempo.sh "$DIR" 2>/dev/null

# Por espectador e fluxo: recebidos, perdidos pela sequência e latência
# média ponderada; por participante: resultados de admissão somados.
for esp in espectador-a espectador-b; do
	LC_ALL=C awk -v esp="$esp" '
	/msg=fluxo/ {
		f = ""; r = 0; p = 0; m = 0; x = 0
		for (i = 1; i <= NF; i++) {
			split($i, kv, "=")
			if (kv[1] == "fluxo") f = kv[2]
			if (kv[1] == "recebidos") r = kv[2]
			if (kv[1] == "perdidos") p = kv[2]
			if (kv[1] == "latencia_media_ms") m = kv[2]
			if (kv[1] == "latencia_max_ms") x = kv[2]
		}
		rec[f] += r; perd[f] += p; soma[f] += m * r
		if (x + 0 > mx[f]) mx[f] = x + 0
	}
	/msg=resumo/ { for (i = 1; i <= NF; i++) { split($i, kv, "="); if (kv[1] == "descartados_fila") desc += kv[2] } }
	END {
		for (f in rec)
			printf "%s %s: recebidos %d, perdidos %d (%.1f%%), latência média %.2f ms, máxima %.1f ms\n", esp, f, rec[f], perd[f], 100 * perd[f] / (rec[f] + perd[f] + 1e-9), soma[f] / (rec[f] + 1e-9), mx[f]
		printf "%s: descartados na fila da inscrição %d\n", esp, desc
	}' "$DIR/$esp.log" | sort
done >"$DIR/fluxos.txt"
for p in $PARTICIPANTES; do
	LC_ALL=C awk -v p="$p" '
	/msg=publicações/ {
		for (i = 1; i <= NF; i++) {
			split($i, kv, "=")
			if (kv[1] != "time" && kv[1] != "level" && kv[1] != "msg" && kv[1] != "cliente" && kv[1] != "topico") n[kv[1]] += kv[2]
		}
	}
	END { printf "%s:", p; for (k in n) printf " %s=%d", k, n[k]; printf "\n" }' "$DIR/$p.log"
done >"$DIR/publicacoes.txt"
python3 scripts/graficos-carga.py "$DIR" >/dev/null 2>&1 || info "gráficos não gerados"

info "registros em $DIR"
cat "$DIR/carga.txt" "$DIR/fluxos.txt" "$DIR/publicacoes.txt" >&2
[ -f "$DIR/justica.csv" ] && grep jain "$DIR/justica.csv" | sed 's/^/carga: índice de Jain (lances no espectador-a) /' >&2
true
