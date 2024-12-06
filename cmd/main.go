package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bredtape/git_sync"
	"github.com/bredtape/slogging"
	"github.com/gorilla/mux"
	"github.com/peterbourgon/ff/v3"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

//go:embed index.html
var indexHTML []byte

type Config struct {
	ListenAddress               string
	TempDir                     string
	EnableHTTPS                 bool
	CertFile, CertServerKeyFile string
}

func (c Config) Validate() error {
	if c.TempDir == "" {
		return fmt.Errorf("temp-dir must be set")
	}
	if c.EnableHTTPS {
		if c.CertFile == "" {
			return fmt.Errorf("cert-file must be set")
		}
		if c.CertServerKeyFile == "" {
			return fmt.Errorf("cert-server-key-file must be set")
		}
	}
	return nil
}

func readArgs() Config {
	envPrefix := "GIT_SYNC"
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.Usage = func() {
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "Options may also be set from the environment. Prefix with %s_, use all caps. and replace any - with _\n", envPrefix)
		os.Exit(1)
	}

	var config Config
	fs.StringVar(&config.ListenAddress, "listen-address", ":8185", "Address to listen on")
	fs.StringVar(&config.TempDir, "temp-dir", "", "Temporary directory for git operations. Will use $TMPDIR if not set")
	fs.BoolVar(&config.EnableHTTPS, "enable-https", false, "Enable HTTPS")
	fs.StringVar(&config.CertFile, "cert-file", "", "Certificate file. Required if enable-https is set")
	fs.StringVar(&config.CertServerKeyFile, "cert-server-key-file", "", "Certificate server key file. Required if enable-https is set")

	var logLevel slog.Level
	fs.TextVar(&logLevel, "log-level", slog.LevelDebug-3, "Log level")
	var logJSON bool
	fs.BoolVar(&logJSON, "log-json", false, "Log in JSON format")
	var help bool
	fs.BoolVar(&help, "help", false, "Show help")

	err := ff.Parse(fs, os.Args[1:], ff.WithEnvVarPrefix(envPrefix))
	if err != nil {
		bail(fs, "parse error: "+err.Error())
		os.Exit(2)
	}

	if help {
		fs.Usage()
		os.Exit(0)
	}
	slogging.SetDefault(logLevel, false, logJSON)

	if config.TempDir == "" {
		config.TempDir = os.TempDir()
	}

	if err := config.Validate(); err != nil {
		bail(fs, "validation error: "+err.Error())
	}

	return config
}

func main() {
	ctx := context.Background()
	config := readArgs()
	log := slog.With("op", "main", "listenAddress", config.ListenAddress, "tempDir", config.TempDir, "enableHTTPS", config.EnableHTTPS)

	mux := mux.NewRouter()
	mux.Handle("/pull", git_sync.NewGitPullHandler(config.TempDir))
	mux.Handle("/push", git_sync.NewGitPushHandler(config.TempDir))
	mux.Handle("/metrics", promhttp.Handler())

	// TODO: Add page at / to explain the endpoints
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/html")
		body := strings.Builder{}
		body.Write(indexHTML)
		w.Write([]byte(body.String()))
	}))

	server := &http.Server{Handler: mux, Addr: config.ListenAddress}

	go func() {
		log.Info("starting server")
		if config.EnableHTTPS {
			err := server.ListenAndServeTLS(config.CertFile, config.CertServerKeyFile)
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("server failed", "error", err)
				os.Exit(2)
			}
		}
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "error", err)
			os.Exit(2)
		}
		log.Log(ctx, slog.LevelDebug-3, "stop serving new connections")
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Debug("shutting down server")
	shutdownCtx, shutdownRelease := context.WithTimeout(ctx, 10*time.Second)
	defer shutdownRelease()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error("HTTP shutdown error", "err", err)
		os.Exit(2)
	}
	log.Info("server stopped")
}

func bail(fs *flag.FlagSet, format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	fs.Usage()
}
