package server

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Chris-Alexander-Pop/gvl/internal/govee"
	"github.com/Chris-Alexander-Pop/gvl/internal/mode"
	"github.com/Chris-Alexander-Pop/gvl/internal/schedule"
)

// Options configures the daemon.
type Options struct {
	Listen         string
	Token          string
	DeviceIP       string
	DataDir        string
	Timezone       string
	AutoDiscover   bool
	DiscoverSubnet string
}

// Server is the gvld HTTP API.
type Server struct {
	opts              Options
	client            *govee.Client
	runner            *mode.Runner
	store             *schedule.Store
	engine            *schedule.Engine
	device            govee.DeviceState
	lastRediscover    time.Time
	lastRediscoverErr error
	mu                sync.Mutex
	deviceMu          sync.Mutex
}

// New creates a server.
func New(opts Options) (*Server, error) {
	if opts.Listen == "" {
		opts.Listen = ":8080"
	}
	if opts.DataDir == "" {
		opts.DataDir = "/data"
	}

	state, err := govee.LoadDeviceState(opts.DataDir)
	if err != nil {
		log.Printf("gvld: load device.json: %v", err)
		state = govee.DeviceState{}
	}

	ip := opts.DeviceIP
	// Persisted IP wins over env when present (DHCP may have moved since last ship).
	if state.IP != "" {
		ip = state.IP
	}
	if ip == "" {
		ip = govee.DefaultDeviceIP()
	}

	client := govee.NewClient(ip)
	runner := mode.NewRunner(client)
	store, err := schedule.NewStore(filepath.Join(opts.DataDir, "schedules.json"))
	if err != nil {
		return nil, err
	}
	engine := schedule.NewEngine(store, runner, client)
	s := &Server{
		opts:   opts,
		client: client,
		runner: runner,
		store:  store,
		engine: engine,
		device: state,
	}
	if s.device.IP == "" && ip != "" {
		s.device.IP = ip
	}
	engine.BeforeFire = s.ensureReachable
	return s, nil
}

// Start begins the schedule loop and HTTP server (blocking).
func (s *Server) Start() error {
	stop := make(chan struct{})
	go s.engine.Run(stop)
	defer close(stop)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/status", s.auth(s.handleStatus))
	mux.HandleFunc("/v1/device", s.auth(s.handleDevice))
	mux.HandleFunc("/v1/stop", s.auth(s.handleStop))
	mux.HandleFunc("/v1/mode", s.auth(s.handleMode))
	mux.HandleFunc("/v1/discover", s.auth(s.handleDiscover))
	mux.HandleFunc("/v1/schedules", s.auth(s.handleSchedules))
	mux.HandleFunc("/v1/schedules/", s.auth(s.handleScheduleItem))

	log.Printf("gvld listening on %s device=%s auto_discover=%v subnet=%q data=%s",
		s.opts.Listen, s.client.IP, s.opts.AutoDiscover, s.opts.DiscoverSubnet, s.opts.DataDir)
	return http.ListenAndServe(s.opts.Listen, mux)
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.opts.Token != "" {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") || strings.TrimPrefix(h, "Bearer ") != s.opts.Token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

func (s *Server) lockDevice(op string) func() {
	if !s.deviceMu.TryLock() {
		log.Printf("gvld: %s waiting (device busy — overlapping commands used to fail on UDP :4002)", op)
		s.deviceMu.Lock()
	}
	start := time.Now()
	return func() {
		log.Printf("gvld: %s %s", op, time.Since(start).Round(time.Millisecond))
		s.deviceMu.Unlock()
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	unlock := s.lockDevice("GET /v1/status")
	defer unlock()
	st, err := s.client.Status(2 * time.Second)
	if err != nil {
		if rerr := s.recoverDevice(err); rerr == nil {
			st, err = s.client.Status(2 * time.Second)
		}
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, 200, map[string]any{
		"ip":     s.client.IP,
		"status": st,
		"mode":   s.runner.Running(),
	})
}

func (s *Server) handleDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cmd, _ := body["cmd"].(string)
	s.runner.Stop()

	unlock := s.lockDevice("POST /v1/device " + cmd)
	defer unlock()

	run := func() (*govee.Status, error) {
		switch cmd {
		case "on":
			return s.client.ExecTurn(true)
		case "off":
			return s.client.ExecTurn(false)
		case "brightness":
			v, _ := asInt(body["value"])
			return s.client.ExecBrightness(v)
		case "color":
			var rgb govee.RGB
			b, _ := json.Marshal(body["color"])
			_ = json.Unmarshal(b, &rgb)
			return s.client.ExecColor(rgb)
		case "temp":
			v, _ := asInt(body["value"])
			return s.client.ExecTemp(v)
		default:
			return nil, errUnknownCmd
		}
	}

	st, err := run()
	if err == errUnknownCmd {
		http.Error(w, "unknown cmd", http.StatusBadRequest)
		return
	}
	if err != nil {
		if rerr := s.recoverDevice(err); rerr == nil {
			st, err = run()
		}
	}
	if err != nil {
		log.Printf("gvld: device %s error: %v", cmd, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, 200, st)
}

var errUnknownCmd = errString("unknown cmd")

type errString string

func (e errString) Error() string { return string(e) }

func asInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case json.Number:
		i, err := t.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.runner.Stop()
	unlock := s.lockDevice("POST /v1/stop")
	defer unlock()
	writeJSON(w, 200, map[string]string{"status": "stopped"})
}

func (s *Server) handleMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var cfg mode.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cfg.Name == "" {
		http.Error(w, "mode name required", http.StatusBadRequest)
		return
	}
	unlock := s.lockDevice("POST /v1/mode " + cfg.Name)
	defer unlock()
	if err := s.ensureReachable(); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.runner.StartMode(cfg)
	writeJSON(w, 200, map[string]string{"mode": cfg.Name, "status": "started"})
}

func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	unlock := s.lockDevice("POST /v1/discover")
	defer unlock()
	devs, err := govee.Discover(3 * time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	_ = govee.CacheIPs(devs)
	s.adoptDiscovered(devs)
	writeJSON(w, 200, devs)
}

func (s *Server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, s.store.List())
	case http.MethodPost, http.MethodPut:
		var e schedule.Entry
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if e.ID == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		if e.Timezone == "" {
			e.Timezone = s.opts.Timezone
		}
		if err := s.store.Upsert(e); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, 200, e)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleScheduleItem(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/schedules/")
	parts := strings.Split(path, "/")
	id := parts[0]
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		e, ok := s.store.Get(id)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, 200, e)
	case action == "" && r.Method == http.MethodPut:
		var e schedule.Entry
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		e.ID = id
		if e.Timezone == "" {
			e.Timezone = s.opts.Timezone
		}
		if err := s.store.Upsert(e); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, 200, e)
	case action == "" && r.Method == http.MethodDelete:
		if err := s.store.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, 200, map[string]string{"deleted": id})
	case action == "enabled" && r.Method == http.MethodPost:
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.store.SetEnabled(id, body.Enabled); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, 200, map[string]any{"id": id, "enabled": body.Enabled})
	case action == "run" && r.Method == http.MethodPost:
		e, ok := s.store.Get(id)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err := s.engine.Fire(e); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, 200, map[string]string{"ran": id})
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// OptionsFromEnv builds options from environment.
func OptionsFromEnv() Options {
	return Options{
		Listen:         envOr("GVL_LISTEN", ":8080"),
		Token:          os.Getenv("GVL_TOKEN"),
		DeviceIP:       os.Getenv("GVL_DEVICE_IP"),
		DataDir:        envOr("GVL_DATA_DIR", "/data"),
		Timezone:       envOr("GVL_TZ", "UTC"),
		AutoDiscover:   envBoolDefaultTrue("GVL_AUTO_DISCOVER"),
		DiscoverSubnet: os.Getenv("GVL_DISCOVER_SUBNET"),
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// envBoolDefaultTrue is true unless explicitly set to 0/false/off/no.
func envBoolDefaultTrue(k string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(k)))
	switch v {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}
