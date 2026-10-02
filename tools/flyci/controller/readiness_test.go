package main

import (
 "context"
 "crypto/ecdsa"
 "crypto/elliptic"
 "crypto/rand"
 "crypto/sha256"
 "crypto/tls"
 "crypto/x509"
 "crypto/x509/pkix"
 "encoding/hex"
 "encoding/json"
 "encoding/pem"
 "errors"
 "io"
 "math/big"
 "net"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func readinessComponentConfig() readinessConfig {
 return readinessConfig{Listen:"127.0.0.1:9443", CertificateFile:"/private/server.pem", KeyFile:"/private/server-key.pem", ClientCAFile:"/private/ca.pem", ClientSPKI:[]string{strings.Repeat("a",64)}, ChainID:125, NetworkID:"component-network", WireVersion:"component-wire", Repository:"component/controller", RunnerApp:"component-runners", RunnerImage:"registry.fly.io/component-runners@sha256:"+strings.Repeat("a",64), SourceRevision:strings.Repeat("b",40), QualificationContract:strings.Repeat("c",64), MaxAgeSeconds:60}
}

func completeReadinessComponent(pass *readinessPass) {
 for operation := range pass.operations { pass.observe(operation, nil) }
 pass.finish(nil)
}

func TestPrivateReadinessObservationStates(t *testing.T) {
 state := newReadinessState(readinessComponentConfig())
 if got := state.reply(); got.Ready || got.Reason != "initial" { t.Fatalf("initial: %+v",got) }
 first := state.begin()
 if got := state.reply(); got.Ready || got.Reason != "in_progress" { t.Fatalf("partial: %+v",got) }
 first.deferJobs(2)
 completeReadinessComponent(first)
 if got := state.reply(); got.PolicyDeferredJobs != 2 { t.Fatal("capacity deferral not retained") }
 if got := state.reply(); !got.Ready || got.ObservedOperations != 8 || got.ObservedAtMS == 0 || got.CompletedAtMS < got.ObservedAtMS || got.ValidUntilMS <= got.CompletedAtMS { t.Fatalf("component completion: %+v",got) }
 next := state.begin()
 if state.reply().Ready { t.Fatal("new incomplete pass reused previous success") }
 first.finish(nil)
 if state.reply().Ready { t.Fatal("superseded pass overwrote current generation") }
 next.finish(nil)
 if state.reply().Ready { t.Fatal("missing required operations accepted") }
 for _, failure := range []string{"github_jit", "fly_create", "fly_destroy", "qualification", "github_history"} {
  pass := state.begin()
  pass.observe(failure, errors.New("component operation failure"))
  pass.observe(failure, nil)
  completeReadinessComponent(pass)
  if state.reply().Ready { t.Fatalf("later success erased failure: %s",failure) }
 }
 pass := state.begin()
 pass.condition("job_index",false)
 pass.condition("job_index",true)
 completeReadinessComponent(pass)
 if state.reply().Ready { t.Fatal("conflict erased") }
 pass = state.begin()
 for operation := range pass.operations { pass.observe(operation,nil) }
 pass.finish(context.Canceled)
 if state.reply().Ready { t.Fatal("cancelled pass accepted") }
 completeReadinessComponent(state.begin())
 state.mu.Lock(); state.snapshot.started = time.Now().Add(-61*time.Second); state.mu.Unlock()
 if got := state.reply(); got.Ready || got.Reason != "stale" { t.Fatalf("stale: %+v",got) }
 completeReadinessComponent(state.begin())
 state.mu.Lock(); state.snapshot.started = time.Now().Add(time.Minute); state.mu.Unlock()
 if state.reply().Ready { t.Fatal("future observation accepted") }
 completeReadinessComponent(state.begin())
 state.unavailable()
 completeReadinessComponent(state.begin())
 if state.reply().Ready { t.Fatal("listener failure erased") }
}

func TestPrivateReadinessQualificationObservation(t *testing.T) {
 for _,status:=range []string{"prepared","dispatch_intent","acknowledgement_unknown","conflict","completed_uncollected","unknown"}{
  state:=newReadinessState(readinessComponentConfig());pass:=state.begin()
  pass.qualificationRecords([]qualificationRecord{{State:status,RunID:1,Attempt:1}})
  completeReadinessComponent(pass)
  if state.reply().Ready {t.Fatalf("partial/conflict qualification admitted: %s",status)}
 }
 state:=newReadinessState(readinessComponentConfig());pass:=state.begin()
 pass.qualificationRecords([]qualificationRecord{{State:"terminal_failed",RunID:1,Attempt:1},{State:"running",RunID:2,Attempt:1}})
 completeReadinessComponent(pass)
 if got:=state.reply();!got.Ready||got.FailedWorkloadJobs!=1{t.Fatalf("workload outcome confused with dependency health: %+v",got)}
 pass=state.begin();pass.qualificationRecords([]qualificationRecord{{State:"run_bound"}});completeReadinessComponent(pass)
 if state.reply().Ready{t.Fatal("missing qualification assignment admitted")}
}

func TestPrivateReadinessConfigurationBinding(t *testing.T) {
 valid := readinessComponentConfig()
 runtime := config{Owner:"component", Repo:"controller", RunnerApp:valid.RunnerApp, RunnerImage:valid.RunnerImage, QualificationRoot:"/private/qualification", QualificationContract:valid.QualificationContract, GitHubAPIURL:defaultGitHubAPIURL, FlyAPIURL:defaultFlyAPIURL, PollInterval:20*time.Second}
 if err := validateReadinessConfig(valid,runtime,valid.SourceRevision); err != nil { t.Fatal(err) }
 cases := map[string]func(*readinessConfig){
  "wildcard":func(c *readinessConfig){c.Listen="0.0.0.0:9443"},
  "ipv6_wildcard":func(c *readinessConfig){c.Listen="[::]:9443"},
  "public":func(c *readinessConfig){c.Listen="8.8.8.8:9443"},
  "source":func(c *readinessConfig){c.SourceRevision=strings.Repeat("d",40)},
  "repository":func(c *readinessConfig){c.Repository="other/repository"},
  "runner":func(c *readinessConfig){c.RunnerApp="other-runners"},
  "image":func(c *readinessConfig){c.RunnerImage="registry.fly.io/component-runners:latest"},
  "network":func(c *readinessConfig){c.NetworkID=""},
  "wire":func(c *readinessConfig){c.WireVersion=""},
  "chain":func(c *readinessConfig){c.ChainID=0},
  "staleness":func(c *readinessConfig){c.MaxAgeSeconds=301},
  "interval":func(c *readinessConfig){c.MaxAgeSeconds=20},
  "client":func(c *readinessConfig){c.ClientSPKI=nil},
  "duplicate_client":func(c *readinessConfig){c.ClientSPKI=[]string{strings.Repeat("a",64),strings.Repeat("a",64)}},
  "qualification":func(c *readinessConfig){c.QualificationContract=strings.Repeat("d",64)},
 }
 for name, change := range cases { t.Run(name,func(t *testing.T){ candidate:=valid; change(&candidate); if validateReadinessConfig(candidate,runtime,valid.SourceRevision)==nil {t.Fatal("invalid binding accepted")} }) }
 if validateReadinessConfig(valid,runtime,"")==nil {t.Fatal("missing immutable source admitted")}
 runtime.GitHubAPIURL="http://127.0.0.1:1"
 if validateReadinessConfig(valid,runtime,valid.SourceRevision)==nil {t.Fatal("unapproved provider origin admitted")}
 runtime.GitHubAPIURL=defaultGitHubAPIURL;runtime.QualificationRoot=""
 if validateReadinessConfig(valid,runtime,valid.SourceRevision)==nil {t.Fatal("missing required qualification admitted")}
}

func TestPrivateReadinessConfigurationPrivacy(t *testing.T) {
 original := config{Owner:"component", Repo:"controller", RunnerApp:"component-runners", RunnerImage:"immutable-component-image", GitHubToken:"private-gh-one", FlyToken:"private-fly-one"}
 changed := original; changed.GitHubToken="private-gh-two"; changed.FlyToken="private-fly-two"
 if readinessRuntimeDigest(original) != readinessRuntimeDigest(changed) { t.Fatal("credential values entered nonsecret configuration fingerprint") }
 changed.RunnerApp="other-component-runners"
 if readinessRuntimeDigest(original) == readinessRuntimeDigest(changed) { t.Fatal("runtime binding absent from fingerprint") }
 if err:=readinessUniqueFields([]byte(`{"network_id":"one","wire_version":"two"}`));err!=nil{t.Fatal(err)}
 for _,raw:=range []string{`{"network_id":"one","network_id":"two"}`,`{"wire_version":"one"} {}`,`[]`}{if readinessUniqueFields([]byte(raw))==nil{t.Fatal("ambiguous configuration admitted")}}
}

func TestPrivateReadinessProtectedFiles(t *testing.T) {
 dir:=t.TempDir();path:=filepath.Join(dir,"binding.json")
 if err:=os.WriteFile(path,[]byte(`{"source_revision":"component"}`),0600);err!=nil{t.Fatal(err)}
 if _,err:=readinessFile(path);err!=nil{t.Fatal(err)}
 if err:=os.Chmod(path,0644);err!=nil{t.Fatal(err)}
 if _,err:=readinessFile(path);err==nil{t.Fatal("unprotected file accepted")}
 if err:=os.Chmod(path,0600);err!=nil{t.Fatal(err)}
 link:=filepath.Join(dir,"symlink");if err:=os.Symlink(path,link);err!=nil{t.Fatal(err)}
 if _,err:=readinessFile(link);err==nil{t.Fatal("symlink accepted")}
 hard:=filepath.Join(dir,"hardlink");if err:=os.Link(path,hard);err!=nil{t.Fatal(err)}
 if _,err:=readinessFile(path);err==nil{t.Fatal("multiply linked file accepted")}
}

type readinessTestCA struct { certificate *x509.Certificate; key *ecdsa.PrivateKey; pem []byte }
func newReadinessTestCA(t *testing.T,serial int64) readinessTestCA {
 t.Helper();key,err:=ecdsa.GenerateKey(elliptic.P256(),rand.Reader);if err!=nil{t.Fatal(err)}
 now:=time.Now();template:=&x509.Certificate{SerialNumber:big.NewInt(serial),Subject:pkix.Name{CommonName:"local readiness test CA"},NotBefore:now.Add(-time.Minute),NotAfter:now.Add(time.Hour),IsCA:true,BasicConstraintsValid:true,KeyUsage:x509.KeyUsageCertSign|x509.KeyUsageCRLSign}
 der,err:=x509.CreateCertificate(rand.Reader,template,template,&key.PublicKey,key);if err!=nil{t.Fatal(err)}
 cert,err:=x509.ParseCertificate(der);if err!=nil{t.Fatal(err)}
 return readinessTestCA{certificate:cert,key:key,pem:pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:der})}
}
func (ca readinessTestCA) leaf(t *testing.T,serial int64,server bool)(tls.Certificate,[]byte,[]byte,string){
 t.Helper();key,err:=ecdsa.GenerateKey(elliptic.P256(),rand.Reader);if err!=nil{t.Fatal(err)}
 now:=time.Now();template:=&x509.Certificate{SerialNumber:big.NewInt(serial),Subject:pkix.Name{CommonName:"readiness peer"},NotBefore:now.Add(-time.Minute),NotAfter:now.Add(time.Hour),KeyUsage:x509.KeyUsageDigitalSignature,ExtKeyUsage:[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
 if server{template.DNSNames=[]string{"localhost"};template.IPAddresses=[]net.IP{net.ParseIP("127.0.0.1")};template.ExtKeyUsage=[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
 der,err:=x509.CreateCertificate(rand.Reader,template,ca.certificate,&key.PublicKey,ca.key);if err!=nil{t.Fatal(err)}
 certPEM:=pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:der});keyDER,err:=x509.MarshalPKCS8PrivateKey(key);if err!=nil{t.Fatal(err)}
 keyPEM:=pem.EncodeToMemory(&pem.Block{Type:"PRIVATE KEY",Bytes:keyDER});pair,err:=tls.X509KeyPair(certPEM,keyPEM);if err!=nil{t.Fatal(err)}
 cert,err:=x509.ParseCertificate(der);if err!=nil{t.Fatal(err)};pin:=sha256.Sum256(cert.RawSubjectPublicKeyInfo)
 return pair,certPEM,keyPEM,hex.EncodeToString(pin[:])
}

func TestPrivateReadinessTLSHandler(t *testing.T){
 ca:=newReadinessTestCA(t,1);_,serverPEM,serverKey,_:=ca.leaf(t,2,true)
 client,_,_,pin:=ca.leaf(t,3,false);outsider,_,_,_:=ca.leaf(t,4,false)
 otherCA:=newReadinessTestCA(t,5);untrusted,_,_,_:=otherCA.leaf(t,6,false)
 cfg:=readinessComponentConfig();cfg.ClientSPKI=[]string{pin};dir:=t.TempDir()
 cfg.CertificateFile=filepath.Join(dir,"server.pem");cfg.KeyFile=filepath.Join(dir,"server-key.pem");cfg.ClientCAFile=filepath.Join(dir,"client-ca.pem")
 for path,data:=range map[string][]byte{cfg.CertificateFile:serverPEM,cfg.KeyFile:serverKey,cfg.ClientCAFile:ca.pem}{if err:=os.WriteFile(path,data,0600);err!=nil{t.Fatal(err)}}
 security,err:=readinessTLS(cfg);if err!=nil{t.Fatal(err)}
 state:=newReadinessState(cfg);server:=httptest.NewUnstartedServer(state);server.TLS=security;server.StartTLS();defer server.Close()
 roots:=x509.NewCertPool();if !roots.AppendCertsFromPEM(ca.pem){t.Fatal("local trust")}
 newClient:=func(certificates []tls.Certificate)*http.Client{transport:=&http.Transport{TLSClientConfig:&tls.Config{RootCAs:roots,Certificates:certificates,MinVersion:tls.VersionTLS13}};t.Cleanup(transport.CloseIdleConnections);return &http.Client{Transport:transport,Timeout:3*time.Second}}
 for name,certs:=range map[string][]tls.Certificate{"missing":nil,"wrong_identity":{outsider},"untrusted":{untrusted}}{t.Run(name,func(t *testing.T){response,err:=newClient(certs).Get(server.URL+"/readyz");if err==nil{response.Body.Close();t.Fatal("unauthorized TLS identity admitted")}})}
 authorized:=newClient([]tls.Certificate{client})
 response,err:=authorized.Get(server.URL+"/readyz");if err!=nil{t.Fatal(err)};if response.StatusCode!=http.StatusServiceUnavailable{t.Fatal(response.Status)};response.Body.Close()
 completeReadinessComponent(state.begin())
 response,err=authorized.Get(server.URL+"/readyz");if err!=nil{t.Fatal(err)}
 var body readinessReply;if err:=json.NewDecoder(response.Body).Decode(&body);err!=nil{t.Fatal(err)};response.Body.Close()
 if response.StatusCode!=200||!body.Ready||body.Binding.NetworkID!=cfg.NetworkID||body.Binding.WireVersion!=cfg.WireVersion||body.SourceRevision!=cfg.SourceRevision||response.Header.Get("Cache-Control")!="no-store"{t.Fatalf("actual component handler: %+v",body)}
 for _,check:=range []struct{method,path string;status int}{{"POST","/readyz",405},{"GET","/admin",404},{"GET","/qualification-client",404},{"GET","/readyz?submit=1",404}}{
  req,err:=http.NewRequest(check.method,server.URL+check.path,nil);if err!=nil{t.Fatal(err)};response,err=authorized.Do(req);if err!=nil{t.Fatal(err)};io.Copy(io.Discard,response.Body);response.Body.Close();if response.StatusCode!=check.status{t.Fatalf("%s %s: %d",check.method,check.path,response.StatusCode)}
 }
 state.begin();response,err=authorized.Get(server.URL+"/readyz");if err!=nil{t.Fatal(err)};response.Body.Close();if response.StatusCode!=503{t.Fatal("in-progress response retained ready")}
}

func TestPrivateReadinessRegistrySnapshot(t *testing.T) {
 root:=t.TempDir()
 if err:=os.Chmod(root,0700);err!=nil {t.Fatal(err)}
 registry:=&qualificationRegistry{root:root}
 lock,err:=registry.lock();if err!=nil {t.Fatal(err)}
 defer lock.Close()
 result:=make(chan error,1)
 go func(){records,err:=readinessQualificationSnapshot(registry);if err==nil && len(records)!=0 {err=errors.New("unexpected empty-registry records")};result<-err}()
 select {case err:=<-result:t.Fatalf("registry snapshot bypassed held lock: %v",err);case <-time.After(20*time.Millisecond):}
 if err:=lock.Close();err!=nil {t.Fatal(err)}
 select {case err:=<-result:if err!=nil {t.Fatal(err)};case <-time.After(2*time.Second):t.Fatal("registry snapshot failed to release lock")}
 records,err:=readinessQualificationSnapshot(registry)
 if err!=nil || len(records)!=0 {t.Fatalf("second real registry snapshot: %v",err)}
}
