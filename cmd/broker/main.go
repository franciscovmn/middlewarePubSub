// Comando broker inicia um broker atendendo o canal de controle dos clientes
// e, se configurado, o canal com o outro broker.
//
// Broker administrativo (A):
//
//	broker -id A -peer-id B -peer-admin -peer-addr hostB:7002
//
// Outro broker (B):
//
//	broker -id B -peer-id A -peer-listen :7002
package main

import (
	"flag"
	"fmt"
	"net/netip"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/franciscovmn/middlewarePubSub/broker"
	"github.com/franciscovmn/middlewarePubSub/internal/logging"
	"github.com/franciscovmn/middlewarePubSub/peer"
)

func main() {
	var (
		id         = flag.String("id", "A", "identificador do broker")
		listen     = flag.String("listen", fmt.Sprintf(":%d", broker.DefaultControlPort), "endereço TCP de comandos")
		eventPort  = flag.Uint("udp-port", broker.DefaultEventPort, "porta UDP de eventos anunciada aos clientes")
		credsPath  = flag.String("credentials", "config/credentials.json", "arquivo JSON de credenciais")
		restricted = flag.Bool("restrict-topic-admin", false, "limita criação e exclusão de tópicos a administradores")

		peerID     = flag.String("peer-id", "", "identificador do outro broker; vazio opera sem par")
		peerAdmin  = flag.Bool("peer-admin", false, "este é o broker administrativo, que disca o par")
		peerAddr   = flag.String("peer-addr", "", "endereço do canal entre brokers do par (broker administrativo)")
		peerListen = flag.String("peer-listen", fmt.Sprintf(":%d", peer.DefaultPort), "endereço do canal entre brokers (outro broker)")
		peerToken  = flag.String("peer-token", "", "segredo compartilhado entre os brokers")
		peerUDP    = flag.String("peer-udp-addr", "", "sobrescreve o endereço UDP de eventos do par, informado por ele no handshake")

		logFormat     = flag.String("log-format", "text", "formato do log: text ou json")
		logLevel      = flag.String("log-level", "info", "nível do log: debug, info, warn ou error")
		memLimit      = flag.Int64("memoria-max-bytes", 256<<20, "teto de memória gerenciada pelo runtime Go")
		dispatchRate  = flag.Int("distribuicao-por-segundo", broker.DefaultDispatchRate, "limite global de datagramas de saída por segundo")
		dispatchBurst = flag.Int("distribuicao-rajada", broker.DefaultDispatchBurst, "rajada inicial do limite global de datagramas de saída")
	)
	flag.Parse()
	if *memLimit < 1 {
		fmt.Fprintln(os.Stderr, "memoria-max-bytes deve ser positivo")
		os.Exit(2)
	}
	debug.SetMemoryLimit(*memLimit)

	log, err := logging.New(os.Stderr, *logFormat, *logLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fatal := func(msg string, args ...any) {
		log.Error(msg, args...)
		os.Exit(1)
	}
	if *eventPort == 0 || *eventPort > 65535 {
		fatal("porta UDP inválida", "porta", *eventPort)
	}
	creds, err := broker.LoadCredentials(*credsPath)
	if err != nil {
		fatal("credenciais", "erro", err)
	}
	cfg := broker.Config{
		ID:                 *id,
		EventPort:          uint16(*eventPort),
		Credentials:        creds,
		RestrictTopicAdmin: *restricted,
		DispatchRate:       *dispatchRate,
		DispatchBurst:      *dispatchBurst,
		Logger:             log,
	}
	if *peerID != "" {
		cfg.Peer = &peer.Config{PeerID: *peerID, Admin: *peerAdmin, DialAddr: *peerAddr, Token: *peerToken}
		if *peerUDP != "" {
			ap, err := netip.ParseAddrPort(*peerUDP)
			if err != nil {
				fatal("endereço UDP do par inválido", "erro", err)
			}
			cfg.Peer.PeerUDPOverride = ap
		}
	}
	b, err := broker.New(cfg)
	if err != nil {
		fatal("configuração", "erro", err)
	}
	log = log.With("broker", *id)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		s := <-sig
		log.Info("sinal recebido, encerrando", "sinal", s.String())
		b.Close()
	}()

	if cfg.Peer != nil && !cfg.Peer.Admin {
		go func() {
			log.Info("aguardando o par", "addr", *peerListen, "par", *peerID)
			if err := b.ListenAndServePeer(*peerListen); err != nil {
				fatal("canal entre brokers", "erro", err)
			}
		}()
	}
	log.Info("broker iniciado", "comandos", *listen, "udp", *eventPort, "par", *peerID,
		"administrativo", cfg.Peer != nil && cfg.Peer.Admin)
	if err := b.ListenAndServe(*listen); err != nil {
		fatal("canal de comandos", "erro", err)
	}
	log.Info("broker encerrado")
}
