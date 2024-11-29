package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/bredtape/git_sync"
	"github.com/gorilla/mux"
	"github.com/peterbourgon/ff/v3"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Config struct {
	ListenAddress string
	SourceRepo    string
	SinkRepo      string
	TempDir       string
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
	fs.StringVar(&config.ListenAddress, "listen-address", ":9180", "Address to listen on")
	fs.StringVar(&config.SourceRepo, "source-repo", "", "Source repository")
	fs.StringVar(&config.SinkRepo, "sink-repo", "", "Sink repository")
	fs.StringVar(&config.TempDir, "temp-dir", "", "Temporary directory for git operations. Will use $TMPDIR if not set")

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

	if config.SourceRepo == "" && config.SinkRepo == "" {
		bail(fs, "either source-repo or sink-repo must be set")
	}

	if config.TempDir == "" {
		config.TempDir = os.TempDir()
	}

	return config
}

func main() {
	config := readArgs()
	log := slog.With("op", "main", "config", config)

	mux := mux.NewRouter()
	if config.SourceRepo != "" {
		// with optional since=<duration> query parameter
		mux.Handle("/pull/{branch}", git_sync.NewGitPullHandler(config.TempDir, config.SourceRepo))
		log.Debug("pull handler registered")
	}
	if config.SinkRepo != "" {
		mux.Handle("/push/{branch}", git_sync.NewGitPushHandler(config.SinkRepo))
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

	log.Info("starting server", "address", config.ListenAddress)
	err := http.ListenAndServe(config.ListenAddress, mux)
	if err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(2)
	}
}

func bail(fs *flag.FlagSet, format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	fs.Usage()
}
