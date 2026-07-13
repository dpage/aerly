// Command server is the Aerly HTTP server.
//
// It serves the React SPA, exposes the JSON API, handles GitHub OAuth, runs
// the flight-tracking poller (OpenSky / stub + dead-reckoning), and
// broadcasts flight updates over Server-Sent Events.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/dpage/aerly/internal/attachments"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/config"
	"github.com/dpage/aerly/internal/db"
	"github.com/dpage/aerly/internal/emailingest"
	"github.com/dpage/aerly/internal/geocode"
	"github.com/dpage/aerly/internal/handlers"
	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/poller"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/migrations"
	"github.com/dpage/aerly/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// Register on the default CommandLine (rather than a private FlagSet) so the
	// flag set already carries the testing package's -test.* flags when main() is
	// driven from a test; a private set would reject them and exit. ExitOnError
	// still prints usage and exits on a genuinely unknown flag in production.
	configPath := flag.String("config", "", "path to a YAML config file (must be mode 0400); "+
		"its keys are env-var names and environment variables override it")
	flag.Parse()

	if err := run(*configPath); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	// Load .env from the current working directory if present, so we don't
	// have to depend on the shell parsing values that contain quotes, $, etc.
	// godotenv's parser handles single-quoted values literally.
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn(".env present but failed to parse", "err", err)
	}
	// An optional --config YAML file fills in any settings the environment (and
	// .env) hasn't already set; env vars take precedence. The file must be
	// 0400 since it may carry secrets, and LoadFile refuses to start otherwise.
	if configPath != "" {
		if err := config.LoadFile(configPath); err != nil {
			return err
		}
		slog.Info("config file loaded", "path", configPath)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(rootCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := db.Migrate(rootCtx, pool, migrations.FS); err != nil {
		return err
	}

	s := store.New(pool)
	authH := auth.NewHandler(cfg.SessionKey, cfg.PublicURL, s)
	authH.MailFromAddress = cfg.MailFromAddress
	authH.SendmailPath = cfg.SendmailPath
	if cfg.GitHubID != "" {
		authH.AddProvider(auth.NewGitHubProvider(cfg.GitHubID, cfg.GitHubSecret))
		slog.Info("auth provider: github")
	}
	if cfg.GoogleID != "" {
		authH.AddProvider(auth.NewGoogleProvider(cfg.GoogleID, cfg.GoogleSecret))
		slog.Info("auth provider: google")
	}
	hub := sse.NewHub()

	// Two resolver handles share one upstream AeroDataBox client. The
	// cached wrapper sits in front of the handler-driven paths (Add Flight
	// dialog, email ingest, flightops) where the 24h TTL hides repeated
	// lookups for the same ident/date. The poller bypasses the cache and
	// uses the raw resolver instead, because (a) it needs fresh airframe
	// data on the day of departure to catch swaps, and (b) it has its own
	// per-flight throttle via last_resolved_at.
	var resolver, rawResolver providers.Resolver
	var adb *providers.AeroDataBox // concrete handle for the quota-alert hook
	if cfg.AeroDataBoxKey != "" {
		adb = providers.NewAeroDataBox(cfg.AeroDataBoxKey)
		rawResolver = adb
		resolver = providers.NewCachedResolver(adb, 24*time.Hour)
		slog.Info("resolver: aerodatabox (cached, ttl=24h; poller uses uncached)")
	}
	api := handlers.New(s, authH, hub, cfg, resolver)
	// Plan attachments (issue #91): wire the configured blob store. nil store
	// (ATTACHMENTS_STORE unset) leaves the upload endpoints reporting disabled.
	if cfg.AttachmentsEnabled() {
		blobStore, err := buildAttachmentStore(cfg)
		if err != nil {
			return err
		}
		api.Attachments = blobStore
		if cfg.AttachmentsIsS3() {
			slog.Info("attachments: s3", "store", cfg.AttachmentsStore)
		} else {
			slog.Info("attachments: filesystem", "path", cfg.AttachmentsStore)
		}
	}
	// Airport lookups go straight to the upstream client (not the cached/raw
	// flight resolver): the endpoint is date-free and its data is static, so it
	// backfills off-table airports on flights outside the ±180-day flight window.
	// nil adb (no key) leaves the fallback disabled.
	if adb != nil {
		api.AirportResolver = adb
	}
	// Geocode part addresses (hotels, taxis, …) into map coordinates via the
	// public OSM Nominatim service. The User-Agent identifies us per policy.
	api.Geocoder = geocode.NewNominatim("aerly (+" + cfg.PublicURL + ")")
	// POI lookups via OpenStreetMap Overpass — no key, cached for 7 days.
	api.Overpass = providers.NewCachedPOIs(
		providers.NewOverpass(cfg.OverpassURL, "aerly (+"+cfg.PublicURL+")"),
		7*24*time.Hour,
	)
	// One-off, best-effort, idempotent startup backfills: geocode any addressed
	// parts still missing coordinates (e.g. ingested before address geocoding),
	// then anchor any parts that have coordinates but no timezone to their real
	// zone. Geocode first so newly-filled coordinates can drive tz resolution.
	go func() {
		api.BackfillPartCoordinates(context.Background())
		api.BackfillPartTimezones(context.Background())
		api.BackfillTripCountries(context.Background())
		// After coordinates exist, fill a destination (and flag) for trips that
		// lack one — notably calendar imports, whose feeds carry no destination.
		api.BackfillTripDestinations(context.Background())
		// Finally, correct trips whose flag/destination were derived before all
		// their plans were plotted (a frozen wrong country, or a destination that
		// points back at the origin). One-shot per trip.
		api.ReconcileTripPlaces(context.Background())
	}()

	// Pick the upstream tracker. OpenSky if credentials are configured (or
	// anonymous OpenSky if requested), otherwise the in-memory stub. The
	// OpenSky path is gated through a SpeedGate first — OpenSky's
	// /states/all?icao24=… happily returns the wrong aircraft when an
	// airframe is reused for a different sector, and the resulting
	// teleport would otherwise pollute the stored track. Either tracker is
	// then wrapped with DeadReckoner so coverage gaps (and gate rejections)
	// fall back to an extrapolation.
	var inner providers.Tracker
	var osky *providers.OpenSky // concrete handle for the quota-alert hook
	switch {
	case cfg.UseOpenSky():
		osky = providers.NewOpenSky(cfg.OpenSkyUsername, cfg.OpenSkyPassword)
		inner = providers.NewSpeedGate(osky, s)
		slog.Info("tracker: opensky",
			"authed", cfg.OpenSkyUsername != "")
	default:
		inner = providers.NewStub()
		slog.Info("tracker: stub")
	}
	tracker := providers.NewDeadReckoner(inner, s)
	p := poller.New(s, tracker, hub, cfg.PollInterval)
	// Give the poller the *uncached* resolver so its day-of refresh sees
	// fresh AeroDataBox state (last_resolved_at handles throttling). Falls
	// back to the cached one when no upstream is configured (i.e. nil).
	p.Resolver = rawResolver
	// The date-free airport fallback for the coord sweep (off-table airports on
	// flights outside the ±180-day flight window). nil adb leaves it disabled.
	if adb != nil {
		p.AirportResolver = adb
	}
	// Flight-alert email config (spec §9). Empty MailFromAddress disables the
	// email channel; in-app alerts still publish over the hub.
	p.MailFromAddress = cfg.MailFromAddress
	p.SendmailPath = cfg.SendmailPath
	p.PublicURL = cfg.PublicURL
	// Web Push is the third alert channel. Reuse the API's Sender (stateless and
	// concurrency-safe); a no-op unless VAPID keys are configured.
	p.Push = api.Push
	// Reuse the API's feed service so the periodic feed-refresh sweep shares the
	// same SSRF-guarded fetcher and store as the on-demand (add/edit) refreshes.
	p.Feeds = api.Feeds

	// Operational quota/rate-limit alerts: when an upstream data provider
	// returns a 429, email the admins (superusers with a verified address) so
	// they can raise the plan tier or back off. Wired as the providers'
	// OnRateLimit hook so it fires even though the tracker layer swallows the
	// error to dead-reckon. A no-op until MAIL_FROM_ADDRESS is configured.
	quota := &poller.QuotaNotifier{
		Store:           s,
		MailFromAddress: cfg.MailFromAddress,
		SendmailPath:    cfg.SendmailPath,
		PublicURL:       cfg.PublicURL,
	}
	if osky != nil {
		osky.OnRateLimit = quota.Notify
	}
	if adb != nil {
		adb.OnRateLimit = quota.Notify
	}

	go p.Run(rootCtx)

	// A configured LLM enables the paste/upload ingest extractor (the HTTP
	// propose/confirm endpoints) independent of email ingest; without one the
	// endpoints stay nil and return 503. Email ingest reuses the same extractor.
	var extractor *emailingest.Extractor
	if cfg.LLMConfigured() {
		llmClient, err := emailingest.NewRealLLM(cfg.LLMProvider, cfg.LLMModel, cfg.LLMAPIKey)
		if err != nil {
			return err
		}
		extractor = emailingest.NewExtractor(llmClient, cfg.LLMModel)
		api.Extractor = extractor
		slog.Info("ingest extractor configured", "llm", cfg.LLMProvider+"/"+cfg.LLMModel)
	}

	if cfg.EmailIngestEnabled {
		if resolver == nil {
			return errors.New("EMAIL_INGEST_ENABLED=1 requires a configured resolver (set AERODATABOX_RAPIDAPI_KEY)")
		}
		svc := &emailingest.Service{
			Cfg: emailingest.Config{
				MaildirPath:     cfg.EmailIngestMaildir,
				PollInterval:    cfg.EmailIngestPollInterval,
				RequireDKIM:     cfg.EmailIngestRequireDKIM,
				DKIMAuthServID:  cfg.EmailIngestDKIMAuthServID,
				RateLimitPerDay: cfg.EmailIngestRateLimitPerDay,
				MaxBodyBytes:    cfg.EmailIngestMaxBodyBytes,
				MaxAttachments:  cfg.EmailIngestMaxAttachments,
				MaxAttachBytes:  cfg.EmailIngestMaxAttachBytes,
				IngestAddress:   cfg.EmailIngestAddress,
				SendmailPath:    cfg.EmailIngestSendmail,
				PublicURL:       cfg.PublicURL,
			},
			Store:     s,
			Extractor: extractor,
			PlanDeps:  planops.Deps{Store: s, Extractor: extractor, Resolver: resolver},
			// Geocode addressed parts + publish live updates, mirroring the HTTP
			// confirm path so emailed hotels/transfers plot on the map and new
			// trips/plans appear without a manual refresh.
			Geocoder: api.Geocoder,
			Hub:      hub,
		}
		go func() {
			if err := svc.Run(rootCtx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("emailingest: stopped", "err", err)
			}
		}()
		slog.Info("emailingest: started",
			"maildir", cfg.EmailIngestMaildir,
			"address", cfg.EmailIngestAddress,
			"llm", cfg.LLMProvider+"/"+cfg.LLMModel)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	authH.Register(mux)
	if cfg.DevAuthBypass {
		authH.RegisterDevLogin(mux)
	}
	api.Register(mux)

	spa, err := web.FS()
	if err != nil {
		return err
	}
	mux.Handle("/", handlers.SPAHandler(spa))

	srv := &http.Server{
		Addr: cfg.ListenAddr,
		// Wrap the whole mux so every response — SPA, API, OAuth, SSE — carries
		// the hardening headers. HSTS is emitted only for an HTTPS deployment.
		Handler:           handlers.SecurityHeaders(mux, cfg.HTTPS()),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.ListenAddr, "public_url", cfg.PublicURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-rootCtx.Done():
		slog.Info("shutdown signal received")
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// buildAttachmentStore constructs the blob store named by ATTACHMENTS_STORE: a
// filesystem directory, or an S3 (or S3-compatible) bucket. The caller has
// already validated the config, so the only errors here are I/O/connection
// setup failures.
func buildAttachmentStore(cfg *config.Config) (attachments.Storage, error) {
	if cfg.AttachmentsIsS3() {
		bucket, prefix := attachments.ParseS3URL(cfg.AttachmentsStore)
		return attachments.NewS3Store(attachments.S3Config{
			Bucket:    bucket,
			Prefix:    prefix,
			Endpoint:  cfg.AttachmentsS3Endpoint,
			Region:    cfg.AttachmentsS3Region,
			AccessKey: cfg.AttachmentsS3AccessKey,
			SecretKey: cfg.AttachmentsS3SecretKey,
			UseSSL:    cfg.AttachmentsS3UseSSL,
		})
	}
	return attachments.NewFSStore(cfg.AttachmentsStore)
}
