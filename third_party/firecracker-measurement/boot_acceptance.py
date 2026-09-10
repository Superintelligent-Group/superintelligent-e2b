import argparse, hashlib, http.client, json, os, pathlib, pty, re, select, socket, struct, subprocess, threading, time, traceback

p = argparse.ArgumentParser()
p.add_argument('--binary', required=True)
p.add_argument('--sha256', required=True)
p.add_argument('--assets', required=True)
p.add_argument('--out', required=True)
a = p.parse_args()
out = pathlib.Path(a.out); out.mkdir(parents=True, exist_ok=False)
assets = pathlib.Path(a.assets)
sha = lambda path: hashlib.file_digest(open(path, 'rb'), 'sha256').hexdigest()
assert sha(a.binary) == a.sha256, 'binary hash mismatch'
manifest = json.loads((assets/'manifest.json').read_text(encoding='utf-8-sig'))
for asset in manifest['assets']:
    assert (assets/asset['name']).stat().st_size == asset['bytes']
    assert sha(assets/asset['name']) == asset['sha256']
result = {'started_at':time.time(), 'binary_sha256':a.sha256, 'assets':manifest, 'passed':False}
result['harness_sha256']=sha(__file__)
serial = bytearray(); frames = []; raw = bytearray(); api_log = []
stop = threading.Event(); proc = None; host_ping = None; serial_thread = None; fifo_thread = None
traffic_stop = threading.Event()
master = slave = readfd = keeper = None
sockpath = '/tmp/sup909-boot.sock'; fifo = '/tmp/sup909-boot.fifo'
class UnixHTTP(http.client.HTTPConnection):
    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout); self.sock.connect(sockpath)
def api(method, path, body):
    conn = UnixHTTP('localhost', timeout=15)
    conn.request(method, path, json.dumps(body) if body is not None else None, {'Content-Type':'application/json'})
    response = conn.getresponse(); data = response.read().decode(); status=response.status; conn.close()
    parsed = json.loads(data) if data else None
    api_log.append({'time':time.time(),'method':method,'path':path,'request':body,'status':status,'response':parsed})
    (out/'api.json').write_text(json.dumps(api_log,indent=2))
    return status, parsed
def good(method,path,body):
    status,data=api(method,path,body); assert 200<=status<300,(path,status,data); return data
def wait_for(predicate, timeout, label):
    until=time.monotonic()+timeout
    while time.monotonic()<until:
        if predicate(): return
        if proc and proc.poll() is not None: raise RuntimeError(f'FC exited {proc.returncode}: {label}')
        time.sleep(.05)
    raise TimeoutError(label)
def serial_reader():
    with open(out/'serial.log','wb',buffering=0) as log:
        while not stop.is_set():
            if select.select([master],[],[],.1)[0]:
                try: data=os.read(master,65536)
                except OSError: break
                if not data: break
                serial.extend(data); log.write(data)
def fifo_reader():
    pending=bytearray()
    with open(out/'metrics.raw','wb',buffering=0) as log:
        while True:
            if select.select([readfd],[],[],.1)[0]:
                try: data=os.read(readfd,65536)
                except BlockingIOError: continue
                if data:
                    raw.extend(data); log.write(data); pending.extend(data)
                    while b'\n' in pending:
                        line,_,tail=pending.partition(b'\n'); pending=bytearray(tail)
                        try: frames.append(json.loads(line))
                        except Exception: frames.append({'parse_error':line.decode(errors='replace')})
                    continue
            if stop.is_set(): break
        if pending: frames.append({'partial_tail':pending.decode(errors='replace')})
def command(text): os.write(master,(text+'\n').encode())
def marker(text): return re.search(rb'(?m)^'+text.encode()+rb'\r*$',bytes(serial)) is not None
def probe(identifier):
    replies=[]
    with socket.socket(socket.AF_INET,socket.SOCK_RAW,socket.IPPROTO_ICMP) as icmp:
        icmp.settimeout(.1)
        for seq in range(3):
            packet=struct.pack('!BBHHH',8,0,0,identifier,seq)+b'SUP909_LOCAL_PROBE'
            padded=packet+b'\0'*(len(packet)%2)
            total=sum(struct.unpack('!'+str(len(padded)//2)+'H',padded))
            total=(total>>16)+(total&65535); total+=(total>>16)
            packet=packet[:2]+struct.pack('!H',(~total)&65535)+packet[4:]
            icmp.sendto(packet,('192.168.0.2',0))
            deadline=time.monotonic()+.6
            while time.monotonic()<deadline:
                try: response,addr=icmp.recvfrom(65536)
                except socket.timeout: continue
                offset=(response[0]&15)*4
                if len(response)>=offset+8:
                    typ,code,_,replyid,replyseq=struct.unpack('!BBHHH',response[offset:offset+8])
                    if typ==0 and code==0 and replyid==identifier and replyseq==seq and addr[0]=='192.168.0.2':
                        replies.append(seq);break
    return {'identifier':identifier,'sent':3,'reply_sequences':replies}
try:
    subprocess.run(['ip','tuntap','add','dev','sup909tap','mode','tap'],check=True)
    subprocess.run(['ip','addr','add','192.168.0.1/30','dev','sup909tap'],check=True)
    subprocess.run(['ip','link','set','sup909tap','up'],check=True)
    os.mkfifo(fifo,0o600); readfd=os.open(fifo,os.O_RDONLY|os.O_NONBLOCK); keeper=os.open(fifo,os.O_RDWR|os.O_NONBLOCK)
    master,slave=pty.openpty()
    proc=subprocess.Popen([a.binary,'--api-sock',sockpath],stdin=slave,stdout=slave,stderr=slave,close_fds=True)
    os.close(slave); slave=None
    serial_thread=threading.Thread(target=serial_reader); serial_thread.start()
    fifo_thread=threading.Thread(target=fifo_reader); fifo_thread.start()
    wait_for(lambda:os.path.exists(sockpath),10,'API socket')
    good('PUT','/metrics',{'metrics_path':fifo,'measurement_incarnation':'sup909_local_boot'})
    req=lambda seq,name:{'incarnation':'sup909_local_boot','request_sequence':seq,'request_id':name}
    baseline=good('PUT','/actions',{'action_type':'FlushMeasurement','measurement':req(1,'baseline')})
    wait_for(lambda:any(f.get('measurement')==baseline for f in frames),5,'baseline frame')
    good('PUT','/machine-config',{'vcpu_count':1,'mem_size_mib':256})
    good('PUT','/boot-source',{'kernel_image_path':str(assets/'vmlinux-5.10.245'),'boot_args':'console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda ro rootfstype=squashfs init=/bin/sh'})
    good('PUT','/drives/rootfs',{'drive_id':'rootfs','path_on_host':str(assets/'ubuntu-24.04.squashfs'),'is_root_device':True,'is_read_only':True})
    good('PUT','/network-interfaces/eth0',{'iface_id':'eth0','host_dev_name':'sup909tap','guest_mac':'06:00:c0:a8:00:02'})
    good('PUT','/actions',{'action_type':'InstanceStart'})
    wait_for(lambda:b'job control' in serial or b'/bin/sh:' in serial,40,'guest shell')
    command("mount -t proc proc /proc; mount -t sysfs sysfs /sys; ip addr add 192.168.0.2/30 dev eth0; ip link set eth0 up; printf '\\nBOOT_GUEST_READY\\n'")
    wait_for(lambda:marker('BOOT_GUEST_READY'),10,'guest execution marker')
    command("ping -c 3 -W 2 192.168.0.1; printf '\\nPING_RESULT_%s\\n' \"$?\"")
    wait_for(lambda:marker('PING_RESULT_0'),15,'guest ping positive control')
    result['host_probe_before']=probe(19090)
    assert len(result['host_probe_before']['reply_sequences'])==3,'host ICMP positive control'
    observation=good('PUT','/actions',{'action_type':'FlushMeasurement','measurement':req(2,'traffic')})
    wait_for(lambda:any(f.get('measurement')==observation for f in frames),5,'traffic frame')
    traffic=next(f for f in frames if f.get('measurement')==observation)
    assert traffic['metrics']['net']['tx_bytes_count']>0 and traffic['metrics']['net']['rx_bytes_count']>0,traffic
    command('ping -i 0.05 -s 256 192.168.0.1 >/dev/null 2>&1 &')
    def host_traffic():
        with socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as udp:
            while not traffic_stop.wait(.05): udp.sendto(b'x'*256,('192.168.0.2',12345))
    host_ping=threading.Thread(target=host_traffic); host_ping.start()
    time.sleep(.5)
    terminal_body={'action_type':'FinalizeMeasurement','measurement':req(3,'terminal')}
    terminal=good('PUT','/actions',terminal_body)
    wait_for(lambda:any(f.get('measurement')==terminal['measurement'] for f in frames),5,'terminal frame')
    terminal_frame=next(f for f in frames if f.get('measurement')==terminal['measurement'])
    assert terminal_frame.get('terminal_cutoff')=='last_completed_device_operation',terminal_frame
    assert not terminal['measurement']['loss_detected'],terminal
    end=len(raw); count=len(frames)
    assert good('PUT','/actions',terminal_body)==terminal
    for method,path,body in [('PATCH','/vm',{'state':'Resumed'}),('PUT','/actions',{'action_type':'FlushMetrics'}),('PUT','/snapshot/create',{'snapshot_type':'Full','snapshot_path':'/tmp/denied.snap','mem_file_path':'/tmp/denied.mem'})]:
        status,data=api(method,path,body)
        assert status==400 and data.get('fault_message')=='Internal VMM error: Terminal measurement latch prohibits resuming the microVM.',(path,status,data)
    result['host_probe_after']=probe(19091)
    assert result['host_probe_after']['reply_sequences']==[],'guest responded after terminal'
    time.sleep(3)
    assert proc.poll() is None, 'Firecracker exited during post-cutoff observation'
    post_cutoff_instance = good('GET', '/', None)
    assert post_cutoff_instance['state'] == 'Paused', post_cutoff_instance
    assert proc.poll() is None, 'Firecracker exited after post-cutoff state read'
    result['post_cutoff_instance'] = post_cutoff_instance
    result['post_cutoff_process_alive'] = True
    assert len(raw)==end and len(frames)==count,'post-terminal emission'
    assert sum(f.get('measurement')==terminal['measurement'] for f in frames)==1,'duplicate terminal frame'
    result.update(passed=True,baseline=baseline,traffic_receipt=observation,terminal=terminal,post_cutoff_observation_seconds=3)
except Exception:
    result['failure']=traceback.format_exc()
finally:
    if host_ping:
        traffic_stop.set();host_ping.join(timeout=2)
    if proc and proc.poll() is None:
        proc.terminate()
        try: proc.wait(timeout=5)
        except subprocess.TimeoutExpired: proc.kill();proc.wait(timeout=3)
    if keeper is not None: os.close(keeper)
    stop.set()
    for thread in (fifo_thread,serial_thread):
        if thread: thread.join(timeout=3); assert not thread.is_alive(),'reader join failed'
    for fd in (readfd,master,slave):
        if fd is not None: os.close(fd)
    if result['passed']:
        try:
            assert frames and all('measurement' in f and 'metrics' in f for f in frames),'invalid or partial frame'
            assert [f['measurement']['attempt_sequence'] for f in frames]==list(range(1,len(frames)+1)),'noncontiguous attempts'
            assert all(f['measurement']['incarnation']=='sup909_local_boot' and not f['measurement']['loss_detected'] for f in frames),'identity or loss failure'
            assert frames[-1]['measurement']==result['terminal']['measurement'],'terminal not last'
            assert sum('terminal_cutoff' in f for f in frames)==1,'terminal count'
        except Exception:
            result['passed']=False;result['failure']=traceback.format_exc()
    result['finished_at']=time.time();result['frames']=frames
    result['metrics_sha256']=sha(out/'metrics.raw') if (out/'metrics.raw').exists() else None
    result['serial_sha256']=sha(out/'serial.log') if (out/'serial.log').exists() else None
    result['api_sha256']=sha(out/'api.json') if (out/'api.json').exists() else None
    (out/'result.json').write_text(json.dumps(result,indent=2))
    print(json.dumps({k:v for k,v in result.items() if k not in ('frames','assets')},indent=2))
    if not result['passed']: raise SystemExit(1)
