package main

import (
	"context"
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

type Config struct {
	ListenAddress               string
	SourceRepo                  string
	SinkRepo                    string
	AuthToken                   string
	TempDir                     string
	EnableHTTPS                 bool
	CertFile, CertServerKeyFile string
}

func (c Config) Validate() error {
	if c.AuthToken == "" {
		return fmt.Errorf("auth-token must be set")
	}
	if c.SourceRepo == "" && c.SinkRepo == "" {
		return fmt.Errorf("either source-repo or sink-repo must be set")
	}
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
	fs.StringVar(&config.SourceRepo, "source-repo", "", "Source repository")
	fs.StringVar(&config.SinkRepo, "sink-repo", "", "Sink repository")
	fs.StringVar(&config.AuthToken, "auth-token", "", "Authorization token for http requests. Required")
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

	if config.SourceRepo == "" && config.SinkRepo == "" {
		bail(fs, "either source-repo or sink-repo must be set")
	}

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
	log := slog.With("op", "main", "listenAddress", config.ListenAddress, "sourceRepo", config.SourceRepo, "sinkRepo", config.SinkRepo,
		"tempDir", config.TempDir, "enableHTTPS", config.EnableHTTPS)

	mux := mux.NewRouter()
	if config.SourceRepo != "" {
		repo := git_sync.RemoteRepo{
			Name:      config.SourceRepo,
			URL:       config.SourceRepo,
			AuthToken: config.AuthToken}
		mux.Handle("/pull/{branch}", git_sync.NewGitPullHandler(config.TempDir, repo))
		log.Debug("pull handler registered")
	}
	if config.SinkRepo != "" {
		repo := git_sync.RemoteRepo{
			Name:      config.SinkRepo,
			URL:       config.SinkRepo,
			AuthToken: config.AuthToken}
		mux.Handle("/push/{branch}", git_sync.NewGitPushHandler(config.TempDir, repo))
		log.Debug("push handler registered")
	}

	mux.Handle("/metrics", promhttp.Handler())

	// TODO: Add page at / to explain the endpoints
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/html")

		body := strings.Builder{}
		body.WriteString(`<html><body>
<h1>Git Sync</h1>
Repository path: ` + config.SourceRepo + `
<p>Use the following endpoints to sync git repositories:</p>
<ul>`)

		if config.SourceRepo != "" {
			body.WriteString(`<li><a href="/pull/{branch}">/pull/{branch}</a> - Pull changes from a git repository. With optional since=&ltduration&gt query parameter. Otherwise all is returned</li>
	`)
		}
		if config.SinkRepo != "" {
			body.WriteString(`
	<li><a href="/push/{branch}">/push/{branch}</a> - Push changes to a git repository</li>`)
		}
		body.WriteString(`
</ul>
</body></html>
`)

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
