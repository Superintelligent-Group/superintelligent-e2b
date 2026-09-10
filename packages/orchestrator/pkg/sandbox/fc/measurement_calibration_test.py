"""SUP916 bounded test fixture; no provider or billing interpretation."""
import base64, collections, hashlib, json, pathlib, re, socket, struct, sys, tempfile, threading, time

def sha(raw): return hashlib.sha256(raw).hexdigest()
def save(path, value): path.write_text(json.dumps(value, indent=2)+'\n')

def traffic(directory, phase, interactive=False):
    directory=pathlib.Path(directory)
    frames=[]; errors=[]; stop=threading.Event()
    capture=socket.socket(socket.AF_PACKET,socket.SOCK_RAW,socket.htons(3))
    capture.bind(('tap0',0)); capture.settimeout(.05)
    def record():
        try:
            while not stop.is_set():
                try: raw,address=capture.recvfrom(65536)
                except socket.timeout: continue
                assert len(frames)<512 and len(raw)<65536, 'capture budget/truncation'
                frames.append({'type':address[2],'bytes':len(raw),'sha256':sha(raw),'hex':raw.hex()})
        except BaseException as error: errors.append(repr(error))
    thread=threading.Thread(target=record); thread.start()
    requests=[]; replies=[]
    identifier=916 if phase=='create' else 917
    try:
        if interactive:
            print('ready',flush=True)
            assert input()=='traffic'
        with socket.socket(socket.AF_INET,socket.SOCK_RAW,socket.IPPROTO_ICMP) as sock:
            sock.settimeout(.05)
            for seq in range(24):
                size=(64,512,1200)[seq%3]
                prefix=f'SUP916:{phase}:{seq}:'.encode()
                payload=prefix+b'x'*(size-len(prefix))
                packet=struct.pack('!BBHHH',8,0,0,identifier,seq)+payload
                words=struct.unpack('!'+'H'*(len(packet)//2),packet)
                total=sum(words); total=(total>>16)+(total&65535); total+=(total>>16)
                packet=packet[:2]+struct.pack('!H',(~total)&65535)+packet[4:]
                requests.append({'seq':seq,'payload_bytes':size,'payload_sha256':sha(payload)})
                sock.sendto(packet,('169.254.0.21',0)); deadline=time.monotonic()+1
                matched=False
                while time.monotonic()<deadline:
                    try: raw,addr=sock.recvfrom(65536)
                    except socket.timeout: continue
                    offset=(raw[0]&15)*4
                    if len(raw)<offset+8: continue
                    typ,code,_,rid,rseq=struct.unpack('!BBHHH',raw[offset:offset+8])
                    if (typ,code,rid,rseq,addr[0],raw[offset+8:])==(0,0,identifier,seq,'169.254.0.21',payload):
                        replies.append(seq); matched=True; break
                assert matched, f'missing exact reply {seq}'
                time.sleep(.25) # finite traffic crosses the production 5s periodic interval
        if interactive:
            print('traffic-complete',flush=True)
            assert input()=='stop'
    finally:
        stop.set(); thread.join(timeout=2)
        statistics=capture.getsockopt(263,6,12)
        kernel_packets,kernel_drops=struct.unpack('II',statistics[:8])
        capture.close()
        save(directory/'packets.json',{'identifier':identifier,'phase':phase,'requests':requests,'replies':replies,'frames':frames,'capture_errors':errors,'kernel_packets':kernel_packets,'kernel_drops':kernel_drops})
    assert not thread.is_alive() and not errors
    assert replies==list(range(24))

def identify(raw,phase,direction,identifier):
    kind=raw[12:14]
    if kind==b'\x08\x00':
        assert raw[14]>>4==4 and raw[23]==1,'unexpected IPv4 traffic'
        offset=14+(raw[14]&15)*4
        typ,code,_,rid,seq=struct.unpack('!BBHHH',raw[offset:offset+8])
        assert (typ,code,rid)==(8 if direction=='rx' else 0,0,identifier) and 0<=seq<24
        size=(64,512,1200)[seq%3]; prefix=f'SUP916:{phase}:{seq}:'.encode()
        assert raw[offset+8:]==prefix+b'x'*(size-len(prefix)),'wrong controlled payload'
        assert len(raw)==14+20+8+size and offset==34,'unexpected controlled frame header'
        return {'kind':'controlled_icmp','sequence':seq,'payload_bytes':size}
    if kind==b'\x08\x06':
        assert len(raw)==42 and raw[14:20]==b'\x00\x01\x08\x00\x06\x04'
        operation=int.from_bytes(raw[20:22],'big'); assert operation in (1,2)
        return {'kind':'arp','operation':operation}
    if kind==b'\x86\xdd':
        assert len(raw)>=54 and raw[14]>>4==6
        protocol=raw[20]; offset=54
        if protocol==0:
            protocol=raw[offset]; offset+=(raw[offset+1]+1)*8
        assert protocol==58 and len(raw)>offset
        typ=raw[offset]; assert typ in (130,131,132,133,134,135,136,143),'unexpected IPv6 traffic'
        return {'kind':'icmpv6_control','type':typ}
    raise AssertionError('unclassified Ethernet traffic')

def calls(path, fds):
    assert path.stat().st_size<=32*1024*1024,'trace budget exceeded'
    pending=None; result=[]
    for line in path.read_text().splitlines():
        if '<unfinished ...>' in line:
            assert pending is None,'overlapping unfinished syscall'
            pending=line.split('<unfinished ...>')[0]; continue
        if line.startswith('<... '):
            match=re.match(r'<\.\.\. (\w+) resumed>(.*)',line)
            assert match and pending and pending.startswith(match[1]+'('),'unmatched syscall resume'
            line=pending+match[2]; pending=None
        match=re.match(r'(readv|writev|read|write)\((\d+)(?:<(?:[^<>]|<[^<>]*>)*>)?, (.*)\)\s+=\s+(-?\d+)(.*)$',line)
        if not match:
            assert not any(re.match(r'(?:readv|writev|read|write)\('+fd+r'(?:<|,)',line) for fd in fds.values()),'unparsed relevant syscall'
            continue
        op,fd,args,returned,_=match.groups(); returned=int(returned)
        if fd not in fds.values(): continue
        if returned<0:
            assert fd==fds['tap'] and op.startswith('read') and 'EAGAIN' in line, 'unexpected relevant IO failure'
            continue
        assert '...' not in args,'truncated iovec or payload'
        encoded=re.findall(r'"((?:\\x[0-9a-f]{2})*)"',args)
        assert encoded,'missing full hexadecimal syscall buffer'
        raw=b''.join(bytes.fromhex(value.replace('\\x','')) for value in encoded)
        assert len(raw)>=returned,'captured buffer shorter than returned bytes'
        if op.startswith('write'): assert len(raw)==returned,'partial write unsupported in calibrated pass'
        result.append({'op':op,'fd':fd,'raw':raw[:returned],'returned':returned})
    if pending:
        assert not any(re.match(r'(?:readv|writev|read|write)\('+fd+r'(?:<|,)',pending) for fd in fds.values()),'unfinished relevant IO at detach'
    return result

def verify(base):
    base=pathlib.Path(base); records=[]; raw_hashes={}
    for file in sorted((base/'spool').glob('*.jsonl')):
        raw=file.read_bytes(); assert raw.endswith(b'\n'),'partial journal tail'
        raw_hashes[file.name]=sha(raw); records.extend(json.loads(line) for line in raw.splitlines())
    assert records and all(r['valid'] and not r['complete'] for r in records)
    results=[]; incarnations=[]
    for phase in ('create','resume'):
        directory=base/('calibration-'+phase)
        start=json.loads((directory/'start.json').read_text()); end=json.loads((directory/'end.json').read_text())
        inc=start['Header']['incarnation']; assert end['Header']['incarnation']==inc
        incarnations.append(inc)
        relevant=[r for r in records if r.get('producer',{}).get('incarnation')==inc]
        frames={int(r['sequence']):r for r in relevant if r['kind']=='producer_frame'}
        for fence in (start,end):
            r=frames[fence['FrameSequence']]
            raw=base64.b64decode(r['producer']['raw']); assert sha(raw)==fence['FrameSHA256']
            matches=[c for c in relevant if c['kind']=='producer_correlation' and int(c['sequence'])==fence['CorrelationSequence'] and c['producer']['frameSha256']==sha(raw)]
            assert len(matches)==1,'missing durable exact correlation'
        interval=[r for seq,r in sorted(frames.items()) if start['FrameSequence']<seq<=end['FrameSequence']]
        assert len(interval)>=2,'periodic interleaving not observed'
        measured={'tx':sum(int(r['deltaTxBytes']) for r in interval),'rx':sum(int(r['deltaRxBytes']) for r in interval)}
        fds=json.loads((directory/'fds.json').read_text()); streams=[]; trace_hashes={}
        for file in sorted(directory.glob('trace.*')):
            trace_hashes[file.name]=sha(file.read_bytes()); events=calls(file,fds)
            if events: streams.append(events)
        # On this pinned event-loop producer, relevant metrics and TAP operations
        # must be completed on one thread. Reject rather than invent cross-thread order.
        assert len(streams)==1,'multiple relevant threads require stronger completion ordering'
        events=streams[0]; fifo=b''; markers={}; offset=0
        for index,event in enumerate(events):
            if event['fd']==fds['metrics']:
                assert event['op'].startswith('write')
                fifo+=event['raw']
                while b'\n' in fifo:
                    line,fifo=fifo.split(b'\n',1)
                    if line: markers.setdefault(sha(line+b'\n'),[]).append(index)
        assert not fifo,'partial metrics frame in syscall trace'
        for fence in (start,end): assert len(markers.get(fence['FrameSHA256'],[]))==1,'trace fence missing or duplicated'
        lo=markers[start['FrameSHA256']][0]; hi=markers[end['FrameSHA256']][0]; assert lo<hi
        observed={'tx':0,'rx':0}; tap_frames=[]
        for event in events[lo+1:hi+1]:
            if event['fd']!=fds['tap']: continue
            direction='rx' if event['op'].startswith('read') else 'tx'
            raw=event['raw']; assert len(raw)>=12+14
            # virtio_net_hdr_v1 = 12 bytes; non-GSO ICMP fixture only.
            assert raw[1]==0,'GSO packet outside fixture contract'
            observed[direction]+=len(raw)
            tap_frames.append((direction,sha(raw[12:]),len(raw)-12))
        packets=json.loads((directory/'packets.json').read_text())
        assert not packets['capture_errors'] and packets['replies']==list(range(24))
        # Old run2 lacks statistics; it can be diagnosed offline but never pass
        # this final acceptance contract without a new capture.
        packet_frames=[]; identities={}
        for frame in packets['frames']:
            assert frame['type'] in (0,1,2,4),'unexpected AF_PACKET direction'
            raw=bytes.fromhex(frame['hex']); assert len(raw)==frame['bytes'] and sha(raw)==frame['sha256']
            packet_frames.append(('rx' if frame['type']==4 else 'tx',sha(raw),len(raw)))
            key=packet_frames[-1]
            identities[key]=identify(raw,phase,key[0],packets['identifier'])
        comparison={'phase':phase,'incarnation':inc,'start':start,'end':end,'measured':measured,'observed':observed,'observed_tap_prefix_bytes':12,'prefix_interpretation':'pinned source virtio_net_hdr_v1; observed by exact AF_PACKET suffix match, not independent ioctl negotiation readback','frame_deltas':len(interval),'tap_frames':tap_frames,'packet_frames':packet_frames,'trace_hashes':trace_hashes}
        # Always retain expected/observed evidence before a mismatch assertion.
        save(directory/'comparison.json',comparison)
        missing=collections.Counter(tap_frames)-collections.Counter(packet_frames)
        outside=collections.Counter(packet_frames)-collections.Counter(tap_frames)
        comparison['missing_capture_frames']=list(missing.elements())
        comparison['outside_trace_fence_capture_frames']=[{'frame':key,'identity':identities[key]} for key in outside.elements()]
        comparison['calibrated_frame_identities']=[{'frame':key,'identity':identities.get(key)} for key in tap_frames]
        save(directory/'comparison.json',comparison)
        assert not missing,'independent frame observation mismatch'
        assert observed==measured, f'exact device byte mismatch: {comparison}'
        assert packets.get('kernel_drops')==0,'capture loss not independently excluded'
        assert packets.get('kernel_packets')==len(packets['frames']),'capture did not drain every observed packet'
        # Finite requests/replies, not merely positive aggregate packet counts.
        for direction in ('rx','tx'):
            controlled=[key for key in tap_frames if key[0]==direction and identities[key]['kind']=='controlled_icmp']
            assert sorted(identities[key]['sequence'] for key in controlled)==list(range(24))
            controls=[key for key in tap_frames if key[0]==direction and identities[key]['kind']!='controlled_icmp']
            assert observed[direction]==sum(12+14+20+8+r['payload_bytes'] for r in packets['requests'])+sum(12+key[2] for key in controls)
        assert all(identities[key]['kind']!='controlled_icmp' for key in outside),'controlled traffic outside exact fences'
        results.append(comparison)
    assert incarnations[0]!=incarnations[1],'Resume reused producer incarnation'
    save(base/'calibration-result.json',{'passed':True,'results':results,'raw_reopen_sha256':raw_hashes,'complete':False,'open_gates':['deferred RX','unsent TX','MMDS','transport response loss','SUP914 manifest binding','cloud identity and provider calibration']})
    print('SUP916 exact finite traffic comparisons passed')

def selftest():
    fds={'tap':'14','metrics':'15'}
    def parse(raw):
        with tempfile.NamedTemporaryFile(mode='w',suffix='.sup916-trace',delete=False) as file:
            file.write(raw); name=pathlib.Path(file.name)
        try: return calls(name,fds)
        finally: name.unlink()
    full='readv(14</dev/net/tun>, [{iov_base="\\x61\\x62\\x63", iov_len=4096}], 1) = 3\n'
    assert parse(full)[0]['raw']==b'abc'
    assert parse(full.replace('</dev/net/tun>','</dev/net/tun<char 10:200>>'))==parse(full)
    split='readv(14</dev/net/tun>,  <unfinished ...>\n--- SIGCHLD {si_signo=SIGCHLD} ---\n<... readv resumed>[{iov_base="\\x61\\x62\\x63", iov_len=4096}], 1) = 3\n'
    assert parse(split)==parse(full)
    assert parse('readv(14</dev/net/tun>, [{iov_base=0x123, iov_len=4096}], 1) = -1 EAGAIN (Resource temporarily unavailable)\n')==[]
    for bad in [full.replace('= 3','= 4'),full.replace('\\x63"','\\x63"...'),full.replace('readv','writev').replace('= 3','= 2'),'<... readv resumed>[]) = 3\n','readv(14</dev/net/tun>, <unfinished ...>\n',full.replace('= 3','= -1 EIO (Input/output error)')]:
        try: parse(bad)
        except AssertionError: pass
        else: raise AssertionError('accepted incomplete or invalid trace: '+bad)
    print('SUP916 parser: completion reconstruction and six rejection cases passed')

if __name__=='__main__':
    if sys.argv[1]=='traffic': traffic(sys.argv[2],sys.argv[3])
    elif sys.argv[1]=='capture': traffic(sys.argv[2],sys.argv[3],True)
    elif sys.argv[1]=='verify': verify(sys.argv[2])
    elif sys.argv[1]=='selftest': selftest()
    else: raise ValueError('unknown calibration mode')
