import json
import pathlib
import re
import uuid
from policy import runtime, require, pinned_image, load
from provenance import Tool


PROBE_SOURCE = r'''package main
import("crypto/sha256";"encoding/hex";"encoding/json";"errors";"fmt";"io";"net/http";"net/url";"os";"syscall";"time")
func main(){
 if len(os.Args)!=2 {os.Exit(2)}
 u,e:=url.Parse(os.Args[1]);if e!=nil||u.Scheme!="http"||u.User!=nil||(u.Hostname()!="127.0.0.1"&&u.Hostname()!="::1"){os.Exit(2)}
 f,e:=os.OpenFile("/app/confinement-write-check",os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
 writeDenied:=e!=nil&&(os.IsPermission(e)||errors.Is(e,syscall.EROFS))
 if e==nil {f.Close();os.Remove("/app/confinement-write-check")}
 fd,e:=syscall.Socket(syscall.AF_INET,syscall.SOCK_RAW,syscall.IPPROTO_ICMP)
 socketDenied:=e==syscall.EPERM||e==syscall.EACCES
 if e==nil {syscall.Close(fd)}
 client:=http.Client{Timeout:3*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}
 healthy:=false
 for i:=0;i<10;i++ {response,e:=client.Get(u.String());healthy=e==nil&&response.StatusCode==200;if response!=nil {response.Body.Close()};if healthy {break};time.Sleep(time.Second)}
 app,e:=os.Open("/app/app");if e!=nil {os.Exit(2)};hasher:=sha256.New();_,e=io.Copy(hasher,app);app.Close();if e!=nil {os.Exit(2)}
 data,_:=json.Marshal(map[string]interface{}{"app_write_denied":writeDenied,"raw_socket_denied":socketDenied,"health_ok":healthy,"app_binary_sha256":hex.EncodeToString(hasher.Sum(nil))});fmt.Println(string(data))
 if !writeDenied||!socketDenied||!healthy {os.Exit(1)}
}
'''


def verify(config, image):
    runtime(config['profile'])
    pinned_image(image)
    require(config['profile']['uid'] == 65532, 'unexpected runtime UID')
    docker = Tool.parse(config['docker'])
    security = load(docker.run(['info', '--format', '{{json .SecurityOptions}}'], timeout=30))
    require(type(security) is list and 'name=seccomp,profile=builtin' in security, 'engine default seccomp is not the built-in profile')
    network = config['network']
    require(type(network) is str and re.fullmatch(r'[A-Za-z0-9_.-]+', network), 'invalid confinement network')
    metadata = load(docker.run(['network', 'inspect', network], timeout=30))
    require(len(metadata) == 1 and metadata[0].get('Internal') is True and metadata[0].get('Id') == config['network_id'], 'network egress not confined')
    name = 'kindlelaunch-confinement-' + uuid.uuid4().hex
    profile = config['profile']
    arguments = ['run', '--detach', '--name', name, '--read-only', '--user', '65532:65532', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges=true', '--network', network, '--cpus', str(profile['limits']['cpu']), '--memory', str(profile['limits']['memory']), '--pids-limit', str(profile['limits']['pids'])]
    for mount in profile.get('tmpfs', []):
        arguments.extend(['--tmpfs', mount['path'] + ':rw,noexec,nosuid,nodev,size=' + str(mount['bytes'])])
    if config.get('environment_file'):
        path = pathlib.Path(config['environment_file'])
        require(path.is_absolute() and path.is_file() and not path.is_symlink(), 'missing confined runtime environment')
        arguments.extend(['--env-file', str(path)])
    arguments.append(image)
    try:
        docker.run(arguments, timeout=60)
        actual = load(docker.run(['inspect', name], timeout=30))[0]
        host = actual['HostConfig']
        require(actual['Config']['User'] == '65532:65532' and host['ReadonlyRootfs'] is True and host['Privileged'] is False, 'actual runtime policy mismatch')
        require(host['CapDrop'] == ['ALL'] and not host.get('CapAdd') and host.get('PidMode') != 'host' and host.get('IpcMode') != 'host' and host.get('NetworkMode') == network and not host.get('Devices'), 'actual runtime privilege mismatch')
        require(not host.get('Binds') and not host.get('VolumesFrom') and all(mount.get('Type') == 'tmpfs' and mount.get('Destination') in {item['path'] for item in profile.get('tmpfs', [])} for mount in actual.get('Mounts', [])) and host.get('UTSMode') != 'host' and host.get('UsernsMode') != 'host', 'actual host filesystem or namespace exposed')
        expected_tmpfs = {mount['path']: 'rw,noexec,nosuid,nodev,size=' + str(mount['bytes']) for mount in profile.get('tmpfs', [])}
        require((host.get('Tmpfs') or {}) == expected_tmpfs, 'actual tmpfs differs from approved bounds')
        require(actual['Config'].get('Image') == image, 'actual container image differs from approval')
        require(not any('unconfined' in option for option in host.get('SecurityOpt', [])), 'actual seccomp disabled')
        require('no-new-privileges=true' in host['SecurityOpt'], 'actual privilege escalation allowed')
        require(host['Memory'] == profile['limits']['memory'] and host['PidsLimit'] == profile['limits']['pids'] and host['NanoCpus'] == profile['limits']['cpu'] * 1000000000, 'actual runtime limits mismatch')
        output = docker.run(['exec', name, '/app/confinement-probe', config['health_url']], timeout=45)
        result = load(output)
        require(result == {'app_write_denied': True, 'raw_socket_denied': True, 'health_ok': True, 'app_binary_sha256': config['approved_binary_sha256']}, 'actual confinement probe failed or wrong binary')
        return {'image': image, 'profile': profile, 'container_id': actual['Id'], 'network_id': config['network_id'], 'probe': result, 'docker_sha256': docker.sha256}
    finally:
        docker.run(['rm', '--force', name], timeout=30)
