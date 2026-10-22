package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/je4/utils/v2/pkg/zLogger"
	rawclient "github.com/je4/wsbrokerclient/pkg/client"
	"github.com/je4/wsbrokerdisplayclient/config"
	"github.com/je4/wsbrokerdisplayclient/pkg/displayclient"
	"github.com/je4/wsbrokerdisplayclient/pkg/manager"
	"github.com/je4/wsbrokerdisplayclient/pkg/web"
	"github.com/rs/zerolog"
)

var configfile = flag.String("config", "", "location of toml configuration file")

func main() {
	flag.Parse()

	// 1. Load configuration
	conf, err := config.LoadConfig(*configfile)
	if err != nil {
		log.Fatalf("cannot load configuration: %v", err)
	}

	// 2. Initialize logger
	output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	loggerZero := zerolog.New(output).With().Timestamp().Str("app", "wsbrokerdisplayclient").Logger()
	var logger zLogger.ZLogger = &loggerZero

	addr := conf.Addr
	if addr == "" {
		addr = "localhost:8770"
	}
	extAddr := conf.ExtAddr
	if extAddr == "" {
		extAddr = fmt.Sprintf("http://%s", addr)
	}

	// 3. Initialize WSBroker client
	rawCfg := rawclient.DefaultConfig()
	rawOpts := []rawclient.Option{
		rawclient.WithEndpoint(conf.WSBroker),
		rawclient.WithClientName(conf.Name),
		rawclient.WithAutoReconnect(true),
		rawclient.WithLogger(logger),
	}
	if conf.ReadLimit > 0 {
		rawOpts = append(rawOpts, rawclient.WithReadLimit(conf.ReadLimit))
	}
	if len(conf.Channels) > 0 {
		rawOpts = append(rawOpts, rawclient.WithInitialChannels(conf.Channels...))
	}
	if conf.ClientTLS.Type != "" || conf.ClientTLS.File != nil || conf.ClientTLS.Vault != nil || conf.ClientTLS.Dev != nil || conf.ClientTLS.Env != nil || conf.ClientTLS.ACME != nil {
		rawOpts = append(rawOpts, rawclient.WithTLSCertLoader(&conf.ClientTLS))
	}

	rawClient, err := rawclient.New(rawCfg, rawOpts...)
	if err != nil {
		logger.Fatal().Err(err).Msg("cannot initialize wsbroker client")
	}

	// 4. Wrap with displayclient
	clientOpts := []displayclient.Option{}
	if conf.Timeout > 0 {
		clientOpts = append(clientOpts, displayclient.WithTimeout(conf.Timeout))
	}
	dispClient, err := displayclient.New(rawClient, logger, clientOpts...)
	if err != nil {
		logger.Fatal().Err(err).Msg("cannot initialize display client wrapper")
	}

	// 5. Connect WSBroker
	connectCtx, connectCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := rawClient.Connect(connectCtx); err != nil {
		logger.Warn().Err(err).Msg("initial wsbroker connection failed, will retry in background")
	}
	connectCancel()

	// 6. Initialize Display Manager
	mgr := manager.NewManager(dispClient, conf.Targets, conf.MaxMessages, logger)

	// 7. Initialize Web Controller Server
	webCfg := web.Config{
		ListenAddr: addr,
		ExtAddr:    extAddr,
		ClientName: conf.Name,
		BrokerURL:  conf.WSBroker,
	}
	webServer, err := web.NewServer(webCfg, mgr, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("cannot initialize web server")
	}

	// 8. Start Web Server
	var wg sync.WaitGroup
	webServer.Start(&wg)

	fmt.Println("==================================================")
	fmt.Printf(" WSBroker Display Client Controller Started\n")
	fmt.Printf(" Client:    %s\n", conf.Name)
	fmt.Printf(" Broker:    %s\n", conf.WSBroker)
	fmt.Printf(" Dashboard: %s\n", extAddr)
	fmt.Printf(" Targets:   %v\n", conf.Targets)
	fmt.Println("==================================================")
	fmt.Println("Press Ctrl+C to terminate...")

	// 9. Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	<-done

	fmt.Println("\nShutting down gracefully...")
	webServer.GracefulStop()
	if err := rawClient.Close(); err != nil {
		logger.Warn().Err(err).Msg("error closing wsbroker client")
	}
	wg.Wait()
	fmt.Println("WSBroker Display Client Controller stopped.")
}
