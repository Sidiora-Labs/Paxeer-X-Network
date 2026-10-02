package main

import (
 "context"
 "crypto/sha256"
 "crypto/subtle"
 "crypto/tls"
 "crypto/x509"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "sync"
 "syscall"
 "time"
)

var ciSourceRevision string

type readinessConfig struct {
 runtimeDigest string
 Listen string `json:"listen"`
 CertificateFile string `json:"certificate_file"`
 KeyFile string `json:"key_file"`
 ClientCAFile string `json:"client_ca_file"`
 ClientSPKI []string `json:"client_spki_sha256"`
 ChainID uint64 `json:"chain_id"`
 NetworkID string `json:"network_id"`
 WireVersion string `json:"wire_version"`
 Repository string `json:"repository"`
 RunnerApp string `json:"runner_app"`
 RunnerImage string `json:"runner_image"`
 SourceRevision string `json:"source_revision"`
 QualificationContract string `json:"qualification_contract"`
 MaxAgeSeconds uint32 `json:"max_age_seconds"`
}

func readinessFile(path string) ([]byte, error) {
 if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.HasPrefix(filepath.Base(path), ".env") {
  return nil, errors.New("readiness material path refused")
 }
 file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
 if err != nil { return nil, errors.New("readiness material unavailable") }
 defer file.Close()
 info, err := file.Stat()
 if err != nil { return nil, errors.New("readiness material metadata unavailable") }
 st, ok := info.Sys().(*syscall.Stat_t)
 if !ok || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || st.Nlink != 1 || (st.Uid != 0 && st.Uid != uint32(os.Geteuid())) || info.Size() <= 0 || info.Size() > 65536 {
  return nil, errors.New("readiness material must be protected regular owner file")
 }
 data, err := io.ReadAll(io.LimitReader(file, 65537))
 if err != nil || len(data) > 65536 { return nil, errors.New("readiness material read refused") }
 return data, nil
}

func readinessHex(value string, size int) bool {
 decoded, err := hex.DecodeString(value)
 return err == nil && len(decoded) == size && value == strings.ToLower(value)
}

func readinessUniqueFields(data []byte) error {
 decoder := json.NewDecoder(strings.NewReader(string(data)))
 first, err := decoder.Token()
 if err != nil || first != json.Delim('{') { return errors.New("readiness configuration must be an object") }
 seen := make(map[string]bool)
 for decoder.More() {
  token, err := decoder.Token()
  name, ok := token.(string)
  if err != nil || !ok || seen[name] { return errors.New("readiness duplicate or invalid field") }
  seen[name] = true
  if err := decoder.Decode(new(json.RawMessage)); err != nil { return errors.New("readiness invalid field value") }
 }
 if _, err := decoder.Token(); err != nil { return errors.New("readiness incomplete configuration") }
 if err := decoder.Decode(new(any)); err != io.EOF { return errors.New("readiness trailing configuration") }
 return nil
}

func loadReadinessConfig(path string, cfg config) (*readinessConfig, error) {
 if path == "" { return nil, nil }
 data, err := readinessFile(path)
 if err != nil { return nil, err }
 if err := readinessUniqueFields(data); err != nil { return nil, err }
 var out readinessConfig
 decoder := json.NewDecoder(strings.NewReader(string(data)))
 decoder.DisallowUnknownFields()
 if err = decoder.Decode(&out); err != nil { return nil, errors.New("readiness configuration invalid") }
 if err = decoder.Decode(new(any)); err != io.EOF { return nil, errors.New("readiness configuration trailing data") }
 if err := validateReadinessConfig(out, cfg, ciSourceRevision); err != nil { return nil, err }
 out.runtimeDigest = readinessRuntimeDigest(cfg)
 return &out, nil
}

func readinessRuntimeDigest(cfg config) string {
 public := map[string]any{"repository":cfg.Owner+"/"+cfg.Repo, "runner_app":cfg.RunnerApp, "runner_image":cfg.RunnerImage, "labels":cfg.Labels, "region":cfg.Region, "cpu_kind":cfg.CPUKind, "cpus":cfg.CPUs, "memory_mb":cfg.MemoryMB, "max_machines":cfg.MaxMachines, "poll_interval_ns":int64(cfg.PollInterval), "idle_timeout_seconds":cfg.IdleTimeout, "orphan_ttl_ns":int64(cfg.OrphanTTL), "github_api":cfg.GitHubAPIURL, "fly_api":cfg.FlyAPIURL, "qualification_contract":cfg.QualificationContract}
 data, _ := json.Marshal(public)
 digest := sha256.Sum256(data)
 return hex.EncodeToString(digest[:])
}

func validateReadinessConfig(out readinessConfig, cfg config, sourceRevision string) error {
 host, port, err := net.SplitHostPort(out.Listen)
 ip := net.ParseIP(host)
 number, portErr := strconv.ParseUint(port, 10, 16)
 if err != nil || portErr != nil || number == 0 || ip == nil || ip.IsUnspecified() || !(ip.IsLoopback() || ip.IsPrivate()) {
  return errors.New("readiness listener must use an explicit private address and port")
 }
 if out.ChainID == 0 || strings.TrimSpace(out.NetworkID) != out.NetworkID || out.NetworkID == "" || len(out.NetworkID) > 128 || strings.TrimSpace(out.WireVersion) != out.WireVersion || out.WireVersion == "" || len(out.WireVersion) > 64 {
  return errors.New("readiness deployment identity invalid")
 }
 if !readinessHex(sourceRevision, 20) || out.SourceRevision != sourceRevision {
  return errors.New("readiness source differs from immutable build revision")
 }
 imageName, imageDigest, digestFound := strings.Cut(out.RunnerImage, "@sha256:")
 if !digestFound || !strings.HasPrefix(imageName, "registry.fly.io/") || strings.ContainsAny(imageName, " \t\r\n@") || !readinessHex(imageDigest, 32) || out.Repository != cfg.Owner+"/"+cfg.Repo || out.RunnerApp != cfg.RunnerApp || out.RunnerImage != cfg.RunnerImage {
  return errors.New("readiness runtime configuration binding mismatch")
 }
 if cfg.GitHubAPIURL != defaultGitHubAPIURL || cfg.FlyAPIURL != defaultFlyAPIURL {
  return errors.New("readiness requires the admitted provider origins")
 }
 if cfg.QualificationRoot == "" || !qDigest.MatchString(out.QualificationContract) || out.QualificationContract != cfg.QualificationContract {
  return errors.New("readiness requires the configured qualification capability")
 }
 if out.MaxAgeSeconds == 0 || out.MaxAgeSeconds > 300 || time.Duration(out.MaxAgeSeconds)*time.Second <= cfg.PollInterval {
  return errors.New("readiness freshness must exceed poll interval and be at most five minutes")
 }
 if len(out.ClientSPKI) == 0 || len(out.ClientSPKI) > 8 { return errors.New("readiness gateway client identity required") }
 seen := make(map[string]bool)
 for _, pin := range out.ClientSPKI {
  if !readinessHex(pin, 32) || seen[pin] { return errors.New("readiness gateway client pin invalid") }
  seen[pin] = true
 }
 for _, material := range []string{out.CertificateFile, out.KeyFile, out.ClientCAFile} {
  if !filepath.IsAbs(material) || filepath.Clean(material) != material { return errors.New("readiness TLS material path invalid") }
 }
 return nil
}

type readinessSnapshot struct {
 generation uint64
 started time.Time
 completed time.Time
 complete bool
 reason string
 observed int
 deferred int
 failedJobs int
}

type readinessState struct {
 mu sync.RWMutex
 cfg readinessConfig
 snapshot readinessSnapshot
 listenerFailed bool
}

type readinessPass struct {
 state *readinessState
 generation uint64
 started time.Time
 operations map[string]bool
 failure string
 deferred int
 failedJobs int
}

func newReadinessState(cfg readinessConfig) *readinessState {
 return &readinessState{cfg: cfg, snapshot: readinessSnapshot{reason: "initial"}}
}

func (state *readinessState) begin() *readinessPass {
 state.mu.Lock()
 defer state.mu.Unlock()
 generation := state.snapshot.generation+1
 now := time.Now()
 state.snapshot = readinessSnapshot{generation: generation, started: now, reason: "in_progress"}
 return &readinessPass{state: state, generation: generation, started: now, operations: map[string]bool{
  "github_runs_queued": false, "github_runs_in_progress": false, "fly_machines": false, "qualification": false, "qualification_storage": false, "qualification_inventory": false, "qualification_state": false, "job_index": false,
 }}
}

func (pass *readinessPass) fail(reason string) {
 if pass != nil && pass.failure == "" { pass.failure = reason }
}

func (pass *readinessPass) observe(operation string, err error) {
 if pass == nil { return }
 if _, exists := pass.operations[operation]; !exists { pass.operations[operation] = false }
 if err != nil { pass.fail("dependency_operation_failed"); return }
 pass.operations[operation] = true
}

func (pass *readinessPass) condition(operation string, success bool) {
 if pass == nil { return }
 if !success { pass.operations[operation] = false; pass.fail(operation); return }
 pass.operations[operation] = true
}

func readinessQualificationSnapshot(registry *qualificationRegistry) ([]qualificationRecord, error) {
 lock, err := registry.lock()
 if err != nil { return nil, err }
 defer lock.Close()
 return registry.list()
}

func (pass *readinessPass) qualificationRecords(records []qualificationRecord) {
 if pass == nil { return }
 valid := true
 for _, record := range records {
  switch record.State {
  case "run_bound", "running", "completed_validated":
   if record.RunID <= 0 || record.Attempt <= 0 { valid = false }
  case "terminal_failed":
   if record.RunID <= 0 || record.Attempt <= 0 { valid = false }
   pass.failedJobs++
  default:
   valid = false
  }
 }
 pass.condition("qualification_state", valid)
}

func (pass *readinessPass) deferJobs(count int) { if pass != nil { pass.deferred += count } }

func (pass *readinessPass) finish(err error) {
 if pass == nil { return }
 if err != nil { pass.fail("poll_cancelled_or_failed") }
 for _, succeeded := range pass.operations { if !succeeded { pass.fail("partial"); break } }
 pass.state.mu.Lock()
 defer pass.state.mu.Unlock()
 if pass.state.snapshot.generation != pass.generation { return }
 pass.state.snapshot = readinessSnapshot{generation: pass.generation, started: pass.started, completed: time.Now(), complete: pass.failure == "", reason: pass.failure, observed: len(pass.operations), deferred: pass.deferred, failedJobs: pass.failedJobs}
}

func (state *readinessState) unavailable() {
 state.mu.Lock(); defer state.mu.Unlock()
 state.listenerFailed = true
 state.snapshot.complete = false
 state.snapshot.reason = "listener_failed"
}

type readinessReply struct {
 Ready bool `json:"ready"`
 Reason string `json:"reason"`
 Binding struct {
  ChainID uint64 `json:"chain_id"`
  NetworkID string `json:"network_id"`
  WireVersion string `json:"wire_version"`
 } `json:"binding"`
 SourceRevision string `json:"source_revision"`
 ProvenanceKind string `json:"provenance_kind"`
 ConfigurationSHA256 string `json:"configuration_sha256"`
 Generation uint64 `json:"generation"`
 ObservedAtMS int64 `json:"observed_at_ms"`
 CompletedAtMS int64 `json:"completed_at_ms"`
 ValidUntilMS int64 `json:"valid_until_ms"`
 ObservedOperations int `json:"observed_operations"`
 PolicyDeferredJobs int `json:"policy_deferred_jobs"`
 FailedWorkloadJobs int `json:"failed_workload_jobs"`
}

func (state *readinessState) reply() readinessReply {
 state.mu.RLock(); defer state.mu.RUnlock()
 snapshot := state.snapshot
 out := readinessReply{Ready: snapshot.complete && !state.listenerFailed, Reason: snapshot.reason, SourceRevision: state.cfg.SourceRevision, ProvenanceKind: "configured_binding_and_controller_reconciliation", ConfigurationSHA256: state.cfg.runtimeDigest, Generation: snapshot.generation, ObservedOperations: snapshot.observed, PolicyDeferredJobs: snapshot.deferred, FailedWorkloadJobs: snapshot.failedJobs}
 out.Binding.ChainID = state.cfg.ChainID
 out.Binding.NetworkID = state.cfg.NetworkID
 out.Binding.WireVersion = state.cfg.WireVersion
 if !snapshot.started.IsZero() {
  now := time.Now()
  age := now.Sub(snapshot.started)
  maxAge := time.Duration(state.cfg.MaxAgeSeconds)*time.Second
  out.ObservedAtMS = snapshot.started.UnixMilli()
  out.ValidUntilMS = snapshot.started.Add(maxAge).UnixMilli()
  if age < 0 || age >= maxAge || now.UnixMilli() < out.ObservedAtMS || now.UnixMilli() >= out.ValidUntilMS { out.Ready = false; out.Reason = "stale" }
 }
 if !snapshot.completed.IsZero() { out.CompletedAtMS = snapshot.completed.UnixMilli() }
 if out.Ready { out.Reason = "ready" }
 return out
}

func readinessPeer(conn tls.ConnectionState, pins []string) bool {
 if len(conn.VerifiedChains) == 0 || len(conn.PeerCertificates) == 0 { return false }
 actual := sha256.Sum256(conn.PeerCertificates[0].RawSubjectPublicKeyInfo)
 for _, pin := range pins {
  expected, err := hex.DecodeString(pin)
  if err == nil && len(expected) == len(actual) && subtle.ConstantTimeCompare(expected, actual[:]) == 1 { return true }
 }
 return false
}

func readinessTLS(cfg readinessConfig) (*tls.Config, error) {
 certificate, err := readinessFile(cfg.CertificateFile)
 if err != nil { return nil, err }
 key, err := readinessFile(cfg.KeyFile)
 if err != nil { return nil, err }
 defer clear(key)
 pair, err := tls.X509KeyPair(certificate, key)
 if err != nil { return nil, errors.New("readiness server identity invalid") }
 ca, err := readinessFile(cfg.ClientCAFile)
 if err != nil { return nil, err }
 roots := x509.NewCertPool()
 if !roots.AppendCertsFromPEM(ca) { return nil, errors.New("readiness gateway trust invalid") }
 return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, VerifyConnection: func(conn tls.ConnectionState) error {
  if !readinessPeer(conn, cfg.ClientSPKI) { return errors.New("readiness gateway identity refused") }
  return nil
 }}, nil
}

func (state *readinessState) ServeHTTP(w http.ResponseWriter, request *http.Request) {
 w.Header().Set("Cache-Control", "no-store")
 if request.TLS == nil || !readinessPeer(*request.TLS, state.cfg.ClientSPKI) { http.Error(w, "unauthorized", http.StatusUnauthorized); return }
 if request.URL.Path != "/readyz" || request.URL.RawPath != "" || request.URL.RawQuery != "" { http.NotFound(w, request); return }
 if request.Method != http.MethodGet { w.Header().Set("Allow", "GET"); http.Error(w, "method_not_allowed", http.StatusMethodNotAllowed); return }
 if request.ContentLength > 0 || len(request.TransferEncoding) != 0 { http.Error(w, "body_not_allowed", http.StatusBadRequest); return }
 reply := state.reply()
 w.Header().Set("Content-Type", "application/json")
 if !reply.Ready { w.WriteHeader(http.StatusServiceUnavailable) }
 _ = json.NewEncoder(w).Encode(reply)
}

type readinessListener struct { net.Listener; slots chan struct{} }
type readinessConn struct { net.Conn; slots chan struct{}; once sync.Once }
func (conn *readinessConn) Close() error { err := conn.Conn.Close(); conn.once.Do(func() { <-conn.slots }); return err }
func (listener *readinessListener) Accept() (net.Conn, error) {
 for {
  conn, err := listener.Listener.Accept()
  if err != nil { return nil, err }
  select { case listener.slots <- struct{}{}: return &readinessConn{Conn:conn, slots:listener.slots}, nil; default: _ = conn.Close() }
 }
}

func startReadiness(ctx context.Context, state *readinessState, stop context.CancelFunc) error {
 security, err := readinessTLS(state.cfg)
 if err != nil { return err }
 listener, err := net.Listen("tcp", state.cfg.Listen)
 if err != nil { return errors.New("readiness private listener unavailable") }
 server := &http.Server{Handler: state, TLSConfig: security, ReadHeaderTimeout: 3*time.Second, ReadTimeout: 5*time.Second, WriteTimeout: 5*time.Second, IdleTimeout: 5*time.Second, MaxHeaderBytes: 8192}
 go func() { <-ctx.Done(); state.unavailable(); shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second); defer cancel(); _ = server.Shutdown(shutdown) }()
 go func() {
  err := server.Serve(tls.NewListener(&readinessListener{Listener:listener, slots:make(chan struct{},64)}, security))
  if err != nil && !errors.Is(err, http.ErrServerClosed) { state.unavailable(); stop() }
 }()
 return nil
}

func readinessOperation(kind string, id int64) string { return fmt.Sprintf("%s_%d", kind, id) }
