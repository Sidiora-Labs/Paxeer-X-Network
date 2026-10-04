#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import stat
import subprocess
import time

ROOT = Path(__file__).resolve().parents[3]
DEFAULT = Path('/root/lx-ops/paxeer-x-integration-2026-10-03/task-0.16-hpx-gate')
SOURCES = ('platform/hosted/gateway/src/lib.rs', 'platform/hosted/gateway/src/main.rs',
           'platform/hosted/gateway/src/routes.rs', 'platform/hosted/gateway/src/http.rs',
           'tools/paxeer-x/route-catalogue.json', 'platform/hosted/gateway/tests/hpx_ingress.rs',
           'hpx/registry/main.go', 'hpx/registry/go.mod', 'hpx/hpx', 'tools/qualification/paxeer-x/hpx-ingress.py')
FIXTURE = r'''
package main

import (
    "crypto/ed25519"
    "crypto/rand"
    "crypto/sha256"
    "crypto/tls"
    "crypto/x509"
    "crypto/x509/pkix"
    "math/big"
    "strings"
    "encoding/hex"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"
    "time"
)

func TestHPXIngressProcess(t *testing.T) {
    directory := os.Getenv("PAXEER_X_HPX_GATE_DIRECTORY")
    if directory == "" { t.Fatal("actual protected gate directory required") }
    info, err := os.Stat(directory)
    if err != nil || !info.IsDir() || info.Mode().Perm() & 0077 != 0 { t.Fatal("protected gate directory required") }
    state := filepath.Join(directory,"registry")
    if err := os.Mkdir(state,0700); err != nil { t.Fatal(err) }
    registry, err := openRegistry(state)
    if err != nil { t.Fatal(err) }
    token := make([]byte,32)
    if _, err := rand.Read(token); err != nil { t.Fatal(err) }
    public, _, err := ed25519.GenerateKey(rand.Reader)
    if err != nil { t.Fatal(err) }
    digest := sha256.Sum256(public)
    owner := &server{cfg: config{ChainID:"paxeer_125-1", RegisterTok:hex.EncodeToString(token)},reg:registry}
    mux := http.NewServeMux()
    mux.HandleFunc("/api/register",owner.handleRegister)
    mux.HandleFunc("/api/myip",owner.handleMyIP)
    mux.HandleFunc("/api/nodes",owner.handleNodes)
    mux.HandleFunc("/healthz",owner.handleHealth)
    caPublic, caPrivate, err := ed25519.GenerateKey(rand.Reader)
    if err != nil { t.Fatal(err) }
    authority := &x509.Certificate{SerialNumber:big.NewInt(1),Subject:pkix.Name{CommonName:"HPX isolated ingress authority"},
        NotBefore:time.Now().Add(-time.Minute),NotAfter:time.Now().Add(10*time.Minute),
        KeyUsage:x509.KeyUsageCertSign|x509.KeyUsageDigitalSignature,IsCA:true,BasicConstraintsValid:true}
    caDER, err := x509.CreateCertificate(rand.Reader,authority,authority,caPublic,caPrivate)
    if err != nil { t.Fatal(err) }
    parent, err := x509.ParseCertificate(caDER)
    if err != nil { t.Fatal(err) }
    leafPublic, leafPrivate, err := ed25519.GenerateKey(rand.Reader)
    if err != nil { t.Fatal(err) }
    leaf := &x509.Certificate{SerialNumber:big.NewInt(2),DNSNames:[]string{"localhost"},Subject:pkix.Name{CommonName:"localhost"},
        NotBefore:authority.NotBefore,NotAfter:authority.NotAfter,KeyUsage:x509.KeyUsageDigitalSignature,
        ExtKeyUsage:[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
    leafDER, err := x509.CreateCertificate(rand.Reader,leaf,parent,leafPublic,caPrivate)
    if err != nil { t.Fatal(err) }
    serving := httptest.NewUnstartedServer(mux)
    serving.TLS = &tls.Config{MinVersion:tls.VersionTLS12,Certificates:[]tls.Certificate{{Certificate:[][]byte{leafDER,caDER},PrivateKey:leafPrivate}}}
    serving.StartTLS()
    defer serving.Close()
    ca := filepath.Join(directory,"ca.der")
    if err := os.WriteFile(ca,caDER,0600); err != nil { t.Fatal(err) }
    document := map[string]string{"url":strings.Replace(serving.URL,"127.0.0.1","localhost",1),"ca":ca,"token":owner.cfg.RegisterTok,"node_id":hex.EncodeToString(digest[:20])}
    encoded, err := json.Marshal(document)
    if err != nil { t.Fatal(err) }
    pending := filepath.Join(directory,"ready.pending")
    if err := os.WriteFile(pending,encoded,0600); err != nil { t.Fatal(err) }
    if err := os.Rename(pending,filepath.Join(directory,"ready.json")); err != nil { t.Fatal(err) }
    deadline := time.Now().Add(8*time.Minute)
    for time.Now().Before(deadline) {
        if _, err := os.Stat(filepath.Join(directory,"stop")); err == nil { return }
        time.Sleep(20*time.Millisecond)
    }
    t.Fatal("bounded actual HPX fixture exceeded its lifetime")
}
'''


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def command(args, directory, log, env, timeout):
    with log.open('wb') as output:
        completed = subprocess.run(args, cwd=directory, stdin=subprocess.DEVNULL, stdout=output,
                                   stderr=subprocess.STDOUT, env=env, timeout=timeout, check=False)
    require(completed.returncode == 0, f'exit={completed.returncode} log={log}')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--build', action='store_true')
    parser.add_argument('--repair-hpx-build', action='store_true')
    parser.add_argument('--repair-fixture-build', action='store_true')
    args = parser.parse_args()
    directory = Path(os.environ.get('PAXEER_X_HPX_GATE_DIRECTORY', str(DEFAULT)))
    require(directory.is_absolute() and not any(part.startswith('.env') for part in directory.parts), 'private evidence path required')
    if args.build:
        require(not directory.exists(), 'gate build evidence already exists')
        directory.mkdir(mode=0o700)
    info = directory.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and not info.st_mode & 0o077,
            'owned protected gate directory required')
    env = dict(os.environ, CARGO_TARGET_DIR='/root/lx-target/migration-gateway', CARGO_BUILD_JOBS='2',
               PAXEER_X_HPX_GATE_DIRECTORY=str(directory), GOMAXPROCS='2')
    if args.build or args.repair_hpx_build or args.repair_fixture_build:
        source = directory / 'hpx'
        if args.repair_fixture_build:
            previous = json.loads((directory/'build.json').read_text())
            require(all(previous['sources'][path] == digest(ROOT/path) for path in SOURCES
                        if path != 'tools/qualification/paxeer-x/hpx-ingress.py'),
                    'unchanged retained gateway and HPX source required')
            require(previous['fixture_sha256'] != hashlib.sha256(FIXTURE.encode()).hexdigest(),
                    'fixture repair requires relevant code change')
            for name in ('ready.json','ca.der','registry','gateway-verify.log','hpx-runtime.log'):
                path = directory/name
                if path.exists(): path.rename(directory/(name+'.first-attempt'))
            (source/'hpx_ingress_test.go').write_text(FIXTURE)
        elif args.repair_hpx_build:
            require(not (directory/'build.json').exists(), 'completed build must not run again')
            output = (directory/'gateway-build.log').read_text()
            require('Finished `test` profile' in output and 'Executable tests/hpx_ingress.rs' in output,
                    'retained successful gateway build required')
            completed_at = (directory/'gateway-build.log').stat().st_mtime_ns
            require(all((ROOT/path).stat().st_mtime_ns <= completed_at for path in SOURCES
                        if path.endswith('.rs') or path.endswith('.json')),
                    'gateway source changed since retained build')
            require(all(digest(source/name) == digest(ROOT/'hpx/registry'/name) for name in ('main.go','go.mod')),
                    'HPX source changed since retained copy')
        else:
            source.mkdir(mode=0o700)
            for name in ('main.go','go.mod'):
                shutil.copyfile(ROOT / 'hpx/registry' / name, source / name)
            (source / 'hpx_ingress_test.go').write_text(FIXTURE)
            command(['bash','-n',str(ROOT/'hpx/hpx')],ROOT,directory/'hpx-cli-build.log',env,10)
            command(['/root/.cargo/bin/cargo','test','--locked','--manifest-path','platform/Cargo.toml',
                     '-p','layerx-platform-gateway','--test','hpx_ingress','--no-run'],
                    ROOT,directory/'gateway-build.log',env,600)
        command(['/usr/local/go/bin/go','test','-c','-o',str(directory/'hpx-ingress-fixture')],
                source,directory/('hpx-build-repair-2.log' if args.repair_fixture_build else 'hpx-build.log'),env,180)
        record = {'sources':{path:digest(ROOT/path) for path in SOURCES},
                  'fixture_sha256':digest(source/'hpx_ingress_test.go'),
                  'binary_sha256':digest(directory/'hpx-ingress-fixture'), 'build_exit':0}
        (directory/'build.json').write_text(json.dumps(record)); (directory/'build.json').chmod(0o600)
        print(f'build_exit=0 log={directory}/gateway-build.log log={directory}/hpx-build.log')
        return
    record = json.loads((directory/'build.json').read_text())
    require(record['build_exit'] == 0 and record['sources'] == {path:digest(ROOT/path) for path in SOURCES},
            'gate requires unchanged final built source')
    require(record['fixture_sha256'] == digest(directory/'hpx/hpx_ingress_test.go')
            and record['binary_sha256'] == digest(directory/'hpx-ingress-fixture'), 'actual fixture identity changed')
    require(not (directory/'ready.json').exists(), 'unchanged gate must not run twice')
    with (directory/'hpx-runtime.log').open('wb') as output:
        process = subprocess.Popen([str(directory/'hpx-ingress-fixture'),'-test.run=^TestHPXIngressProcess$','-test.v'],
            cwd=directory/'hpx',stdin=subprocess.DEVNULL,stdout=output,stderr=subprocess.STDOUT,
            env=env,start_new_session=True)
        try:
            deadline = time.monotonic()+20
            while not (directory/'ready.json').exists():
                require(process.poll() is None and time.monotonic()<deadline,'actual HPX TLS process unavailable')
                time.sleep(.02)
            command(['/root/.cargo/bin/cargo','test','--locked','--manifest-path','platform/Cargo.toml',
                     '-p','layerx-platform-gateway','--test','hpx_ingress'],
                    ROOT,directory/'gateway-verify.log',env,300)
            (directory/'stop').touch(mode=0o600)
            require(process.wait(timeout=10) == 0,'actual HPX process did not close cleanly')
        finally:
            if process.poll() is None:
                os.killpg(process.pid,signal.SIGTERM)
                try: process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid,signal.SIGKILL); process.wait(timeout=5)
    print(f'PAXEER_X_GATE tests=5 skipped=0 log={directory}/gateway-verify.log')


if __name__ == '__main__':
    os.umask(0o077)
    def interrupted(_number, _frame):
        raise KeyboardInterrupt()
    signal.signal(signal.SIGTERM, interrupted)
    try:
        main()
    except (Exception, KeyboardInterrupt) as error:
        print('HPX ingress refused: '+str(error))
        raise SystemExit(1)
