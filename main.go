package main

import (
	"context"
	"embed"
	"flag"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/anupcshan/hsadmin/internal/auth"
	"github.com/anupcshan/hsadmin/internal/config"
	"github.com/anupcshan/hsadmin/internal/events"
	"github.com/anupcshan/hsadmin/internal/handlers"
	headscale "github.com/juanfont/headscale/gen/go/headscale/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/timestamppb"
	"tailscale.com/tsnet"
)

//go:embed web/templates/*.html
var templatesFS embed.FS

type apiKeyAuth struct {
	key string
}

func (a *apiKeyAuth) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + a.key}, nil
}

func (a *apiKeyAuth) RequireTransportSecurity() bool {
	return true
}

func main() {
	configPath := flag.String("config", "", "Path to config file")
	flag.Parse()

	if len(*configPath) == 0 {
		log.Fatalf("-config is required")
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	// Connect to Headscale
	conn, err := grpc.NewClient(
		cfg.Headscale.APIHostPort,
		grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")),
		grpc.WithPerRPCCredentials(&apiKeyAuth{key: cfg.Headscale.APIKey}),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	headscaleClient := headscale.NewHeadscaleServiceClient(conn)

	// Create pre-auth key for hsadmin agent
	key, err := headscaleClient.CreatePreAuthKey(context.Background(), &headscale.CreatePreAuthKeyRequest{
		Ephemeral:  true,
		Reusable:   false,
		Expiration: timestamppb.New(time.Now().Add(1 * time.Hour)),
		AclTags:    cfg.Headscale.AgentTags,
		User:       cfg.Headscale.AgentUserID,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Start tsnet server
	tsnetSrv := &tsnet.Server{
		Hostname:   "hsadmin",
		Ephemeral:  true,
		AuthKey:    key.PreAuthKey.Key,
		ControlURL: cfg.Headscale.ServerURL,
	}
	defer tsnetSrv.Close()

	// tsnet listener for Tailscale network access
	tsnetLn, err := tsnetSrv.Listen("tcp", ":80")
	if err != nil {
		log.Fatal(err)
	}
	defer tsnetLn.Close()

	localClient, err := tsnetSrv.LocalClient()
	if err != nil {
		log.Fatal(err)
	}

	// Parse templates from embedded filesystem
	funcMap := template.FuncMap{
		"sub": func(a, b int) int { return a - b },
		"mul": func(a, b float64) float64 { return a * b },
	}
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFS(templatesFS, "web/templates/*.html"))

	// Setup auth middleware (if enabled)
	var authMiddleware *auth.Middleware
	var authHandlers *auth.AuthHandlers
	hasAuth := cfg.Listeners.Tailscale != nil || cfg.Listeners.HTTP != nil
	if hasAuth {
		authMiddleware = auth.NewMiddleware(cfg, localClient)
		log.Printf("Authentication enabled (Tailscale: %v, HTTP: %v)",
			cfg.Listeners.Tailscale != nil,
			cfg.Listeners.HTTP != nil)

		// Setup OIDC auth handlers if HTTP listener with OIDC is configured
		if cfg.Listeners.HTTP != nil && cfg.Listeners.HTTP.OIDC != nil {
			authHandlers = auth.NewAuthHandlers(authMiddleware.GetOIDCAuth(), tmpl)
			log.Printf("OIDC authentication configured with provider: %s", cfg.Listeners.HTTP.OIDC.ProviderURL)
		}
	} else {
		log.Printf("Warning: No authentication configured - all users will have access")
	}

	// Setup handlers
	machinesHandler := handlers.NewMachinesHandler(tmpl, headscaleClient, localClient)
	machineActionsHandler := handlers.NewMachineActionsHandler(tmpl, headscaleClient, localClient)
	usersHandler := handlers.NewUsersHandler(tmpl, headscaleClient, localClient, machinesHandler)

	// Setup SSE
	broker := events.NewBroker()
	defer broker.Close()
	sseHandler := handlers.NewSSEHandler(tmpl, headscaleClient, localClient, broker, machinesHandler, usersHandler)

	// Setup routes
	mux := http.NewServeMux()

	// Auth routes (public, no auth required)
	if authHandlers != nil {
		mux.HandleFunc("/auth/login", authHandlers.ShowLoginPage)
		mux.HandleFunc("/auth/callback", authHandlers.HandleCallback)
		mux.HandleFunc("/auth/logout", authHandlers.HandleLogout)
	}

	// Protected routes
	handlers.SetupRoutes(mux, machinesHandler, machineActionsHandler, usersHandler, sseHandler)

	// Wrap with auth middleware if enabled
	var handler http.Handler = mux
	if authMiddleware != nil {
		handler = authMiddleware.RequireAuth(mux)
	}

	// Signal handling
	ctx, cancelFunc := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		log.Println("Shutting down...")
		cancelFunc()
	}()

	// Start SSE polling loop
	go sseHandler.StartPolling(ctx)

	// Start HTTP servers
	log.Println("Starting hsadmin server...")

	// Start tsnet listener (always - required for tsnet functionality)
	tsnetPort := 80
	if cfg.Listeners.Tailscale != nil && cfg.Listeners.Tailscale.Port != 0 {
		tsnetPort = cfg.Listeners.Tailscale.Port
	}
	log.Printf("Listening on Tailscale network (tsnet :%d)", tsnetPort)
	go func() {
		if err := http.Serve(tsnetLn, handler); err != nil {
			log.Printf("tsnet listener error: %v", err)
		}
	}()

	// Start regular HTTP listener if configured
	if cfg.Listeners.HTTP != nil {
		log.Printf("Listening on %s for external access", cfg.Listeners.HTTP.ListenAddr)
		go func() {
			if err := http.ListenAndServe(cfg.Listeners.HTTP.ListenAddr, handler); err != nil {
				log.Printf("HTTP listener error: %v", err)
			}
		}()
	}

	<-ctx.Done()
	log.Println("Shutdown complete")
}
