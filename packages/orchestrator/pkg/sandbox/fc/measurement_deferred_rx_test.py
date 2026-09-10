"""SUP921 RX-only extension to the SUP920 test parser. No production code."""

RX_IDENTIFIER = 921


def rx_payload(seq):
    prefix = f'SUP921:RX:{seq}:'.encode()
    return prefix + b'x' * (64 - len(prefix))


def checksum(raw):
    raw += b'\0' * (len(raw) % 2)
    value = sum(struct.unpack('!' + 'H' * (len(raw) // 2), raw))
    while value >> 16:
        value = (value & 65535) + (value >> 16)
    return (~value) & 65535


def rx_identify(raw, direction):
    if raw[12:14] != b'\x08\x00':
        return identify(raw, 'deferred-rx', direction, RX_IDENTIFIER)
    assert len(raw) == 14 + 20 + 8 + 64 and raw[14] == 0x45 and raw[23] == 1, 'unexpected IPv4 frame'
    assert int.from_bytes(raw[16:18], 'big') == 92 and raw[20:22] in (b'\0\0', b'\x40\0'), 'fragmented/invalid IPv4'
    expected = ('169.254.0.22', '169.254.0.21') if direction == 'rx' else ('169.254.0.21', '169.254.0.22')
    assert (socket.inet_ntoa(raw[26:30]), socket.inet_ntoa(raw[30:34])) == expected
    typ, code, _, identifier, seq = struct.unpack('!BBHHH', raw[34:42])
    assert (typ, code, identifier) == (8 if direction == 'rx' else 0, 0, RX_IDENTIFIER)
    assert seq in range(4) and raw[42:] == rx_payload(seq), 'wrong controlled payload'
    assert checksum(raw[14:34]) == 0 and checksum(raw[34:]) == 0, 'invalid controlled checksum'
    return {'kind': 'controlled_icmp', 'sequence': seq, 'label': 'ABCD'[seq]}


def deferred_capture(directory):
    directory = pathlib.Path(directory)
    frames = []; errors = []; replies = []; sends = []; stop = threading.Event()
    capture = socket.socket(socket.AF_PACKET, socket.SOCK_RAW, socket.htons(3))
    capture.bind(('tap0', 0)); capture.settimeout(.05)

    def record():
        try:
            while not stop.is_set():
                try: raw, address = capture.recvfrom(65536)
                except socket.timeout: continue
                assert len(frames) < 512 and len(raw) < 65536, 'capture budget/truncation'
                frames.append({'type': address[2], 'bytes': len(raw), 'sha256': sha(raw), 'hex': raw.hex()})
        except BaseException as error: errors.append(repr(error))

    thread = threading.Thread(target=record); thread.start()
    began = time.monotonic()
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_RAW, socket.IPPROTO_ICMP) as sock:
            sock.settimeout(.05)

            def send(seq):
                packet = struct.pack('!BBHHH', 8, 0, 0, RX_IDENTIFIER, seq) + rx_payload(seq)
                packet = packet[:2] + struct.pack('!H', checksum(packet)) + packet[4:]
                assert sock.sendto(packet, ('169.254.0.21', 0)) == len(packet)
                sends.append(seq)

            def receive(seconds):
                deadline = time.monotonic() + seconds
                while time.monotonic() < deadline:
                    try: raw, address = sock.recvfrom(65536)
                    except socket.timeout: continue
                    offset = (raw[0] & 15) * 4
                    if len(raw) < offset + 8: continue
                    typ, code, _, identifier, seq = struct.unpack('!BBHHH', raw[offset:offset+8])
                    if typ == 0 and code == 0 and identifier == RX_IDENTIFIER:
                        assert address[0] == '169.254.0.21' and seq in range(4) and raw[offset+8:] == rx_payload(seq)
                        replies.append(seq)

            print('ready', flush=True)
            assert input() == 'traffic'
            send(0); receive(.5)
            assert replies == [0], 'A must consume the sole RX operation and actually reply'
            send(1); send(2); receive(.5)
            assert replies == [0], 'B/C unexpectedly delivered'
            # Snapshot of independent preterminal capture; final capture remains
            # active. This is readiness evidence, never a final loss-free receipt.
            save(directory/'held-packets.json', {'frames': list(frames), 'replies': list(replies), 'sends': list(sends), 'capture_errors': list(errors)})
            print('traffic-complete', flush=True)
            assert input() == 'post-terminal'
            send(3); receive(1)
            assert replies == [0], 'reply after terminal'
            print('post-terminal-complete', flush=True)
            assert input() == 'stop'
    finally:
        stop.set(); thread.join(timeout=2)
        statistics = capture.getsockopt(263, 6, 12)
        kernel_packets, kernel_drops = struct.unpack('II', statistics[:8])
        capture.close()
        save(directory/'packets.json', {'identifier': RX_IDENTIFIER, 'frames': frames, 'replies': replies, 'sends': sends, 'capture_errors': errors, 'kernel_packets': kernel_packets, 'kernel_drops': kernel_drops, 'elapsed_seconds': time.monotonic()-began})
    assert not thread.is_alive() and not errors


def trace_evidence(directory, fences):
    fds = json.loads((directory/'fds.json').read_text())
    streams = []; hashes = {}
    for file in sorted(directory.glob('trace.*')):
        hashes[file.name] = sha(file.read_bytes())
        events = calls(file, fds)
        if events: streams.append(events)
    assert len(streams) == 1, 'require one completed relevant event-loop stream'
    events = streams[0]; fifo = b''; markers = {}; emitted = []
    for index, event in enumerate(events):
        if event['fd'] != fds['metrics']: continue
        assert event['op'].startswith('write')
        fifo += event['raw']
        while b'\n' in fifo:
            line, fifo = fifo.split(b'\n', 1)
            assert line, 'empty producer frame'
            digest = sha(line+b'\n')
            markers.setdefault(digest, []).append(index)
            emitted.append((index, line+b'\n'))
    assert not fifo, 'partial metrics frame'
    positions = []
    for fence in fences:
        matches = markers.get(fence['FrameSHA256'], [])
        assert len(matches) == 1, 'exact trace fence absent/duplicated'
        positions.append(matches[0])
    assert positions == sorted(set(positions)), 'nonmonotonic fences'
    return fds, events, emitted, positions, hashes


def packet_evidence(packets):
    assert not packets['capture_errors'] and packets['replies'] == [0]
    observed = []; identities = {}
    for frame in packets['frames']:
        assert frame['type'] in (0, 1, 2, 4)
        raw = bytes.fromhex(frame['hex'])
        assert sha(raw) == frame['sha256'] and len(raw) == frame['bytes']
        key = ('rx' if frame['type'] == 4 else 'tx', sha(raw), len(raw))
        observed.append(key); identities[key] = rx_identify(raw, key[0])
    return observed, identities


def tap_evidence(events, fds):
    frames = []; totals = {'rx': 0, 'tx': 0}
    for event in events:
        if event['fd'] != fds['tap']: continue
        raw = event['raw']; assert len(raw) >= 26 and raw[1] == 0, 'unexpected TAP prefix/GSO'
        direction = 'rx' if event['op'].startswith('read') else 'tx'
        frames.append((direction, sha(raw[12:]), len(raw)-12))
        totals[direction] += len(raw)
    return frames, totals


def require_held(tap, packets, identities, emitted):
    assert not (collections.Counter(tap)-collections.Counter(packets)), 'TAP frame lacks independent capture'
    rx = [key for key in tap if key[0] == 'rx']
    # No incidental RX can consume the sole token. TX controls remain explicitly
    # classified/accounted. Reject, rather than infer which frame spent a token.
    assert [identities[key].get('sequence') for key in rx] == [0, 1], 'RX must read exactly A then B; C unread'
    tx = [identities[key]['sequence'] for key in tap if key[0] == 'tx' and identities[key]['kind'] == 'controlled_icmp']
    assert tx == [0], 'only A may have a guest reply'
    assert sorted(identities[key]['sequence'] for key in packets if key[0] == 'rx' and identities[key]['kind'] == 'controlled_icmp') == [0, 1, 2], 'missing/duplicate A/B/C injection'
    metrics = [json.loads(raw)['metrics'] for _, raw in emitted]
    assert sum(m['net']['rx_rate_limiter_throttled'] for m in metrics) > 0, 'no observed throttle'
    for m in metrics:
        assert m['mmds']['rx_accepted'] == 0, 'MMDS outside RX-only contract'
        assert m['net']['tap_read_fails'] == 0 and m['net']['tap_write_fails'] == 0
        assert m['net']['no_rx_avail_buffer'] == 0, 'RX buffer shortage confounds limiter interpretation'


def held_check(directory):
    directory = pathlib.Path(directory)
    fences = [json.loads((directory/(name+'.json')).read_text()) for name in ('start', 'held')]
    deadline = time.monotonic()+3
    while True:
        try:
            fds, events, emitted, positions, _ = trace_evidence(directory, fences)
            lo, hi = positions
            tap, totals = tap_evidence(events[lo+1:hi+1], fds)
            packets = json.loads((directory/'held-packets.json').read_text())
            packet_frames, identities = packet_evidence(packets)
            require_held(tap, packet_frames, identities, [(i, raw) for i, raw in emitted if lo < i <= hi])
            save(directory/'held-readiness.json', {'ready': True, 'final_evidence': False, 'totals': totals, 'start': fences[0], 'held': fences[1], 'interpretation': 'B deferred by pinned source ordering plus one-token configuration, successful read, A reply and throttle; not direct queue inspection'})
            print('SUP921 held prerequisite passed; final detached trace still required')
            return
        except (AssertionError, json.JSONDecodeError) as error:
            # A writer may currently be between trace lines. Retry readiness only;
            # final verification never tolerates an unfinished/truncated trace.
            if time.monotonic() >= deadline: raise AssertionError('held readiness failed: '+str(error)) from error
            time.sleep(.02)


def verify_deferred(base):
    base = pathlib.Path(base); directory = base/'calibration-deferred-rx'
    start, held, terminal = [json.loads((directory/(name+'.json')).read_text()) for name in ('start', 'held', 'terminal')]
    assert start['Scope'] == held['Scope'] == 'device_sample'
    assert terminal['Scope'] == 'terminal_device_cutoff' and terminal['Request']['request_id'] == 'terminal'
    inc = start['Header']['incarnation']; assert held['Header']['incarnation'] == terminal['Header']['incarnation'] == inc
    config = json.loads((directory/'limiter.json').read_text())
    assert config['method'] == 'PATCH' and config['status'] == 204 and config['response'] == ''
    assert config['path'] == '/network-interfaces/'+config['request']['iface_id']
    assert config['request']['rx_rate_limiter'] == {'operations': {'size': 1, 'one_time_burst': 0, 'refill_time': 3600000}}
    records = []; raw_hashes = {}
    for file in sorted((base/'spool').glob('*.jsonl')):
        raw = file.read_bytes(); assert raw.endswith(b'\n'), 'partial journal tail'
        raw_hashes[file.name] = sha(raw)
        records.extend(json.loads(line) for line in raw.splitlines())
    assert records and all(r['valid'] and not r['complete'] for r in records)
    assert [int(r['sequence']) for r in records] == list(range(1, len(records)+1)), 'journal sequence discontinuity'
    frames = {int(r['sequence']): r for r in records if r['kind'] == 'producer_frame'}
    assert all(r['producer']['incarnation'] == inc for r in frames.values())
    for fence in (start, held, terminal):
        record = frames[fence['FrameSequence']]
        raw = base64.b64decode(record['producer']['raw'])
        assert sha(raw) == fence['FrameSHA256'] and len(raw) == fence['FrameBytes']
        assert json.loads(raw)['measurement'] == fence['Header']
        correlations = [r for r in records if r['kind'] == 'producer_correlation' and int(r['sequence']) == fence['CorrelationSequence']]
        assert len(correlations) == 1 and correlations[0]['producer']['frameSha256'] == sha(raw)
    assert terminal['Cutoff'] == 'last_completed_device_operation'
    assert json.loads(base64.b64decode(frames[terminal['FrameSequence']]['producer']['raw']))['terminal_cutoff'] == terminal['Cutoff']
    assert terminal['FrameSequence'] == max(frames), 'frame after terminal'
    fds, events, emitted, positions, trace_hashes = trace_evidence(directory, [start, held, terminal])
    lo, mid, hi = positions
    assert not any(e['fd'] == fds['tap'] for e in events[hi+1:]), 'successful TAP byte operation after cutoff'
    assert not any(i > hi for i, _ in emitted), 'producer emission after cutoff'
    packets = json.loads((directory/'packets.json').read_text())
    packet_frames, identities = packet_evidence(packets)
    assert packets['sends'] == [0, 1, 2, 3] and 1 <= packets['elapsed_seconds'] < 60
    assert packets['kernel_drops'] == 0 and packets['kernel_packets'] == len(packets['frames']), 'capture loss/undrained packets'
    assert sorted(identities[k]['sequence'] for k in packet_frames if k[0] == 'rx' and identities[k]['kind'] == 'controlled_icmp') == [0, 1, 2, 3]
    held_packets = json.loads((directory/'held-packets.json').read_text())
    held_frames, held_identities = packet_evidence(held_packets)
    assert held_packets['sends'] == [0, 1, 2]
    assert not (collections.Counter(held_frames)-collections.Counter(packet_frames)), 'held capture changed'
    held_tap, _ = tap_evidence(events[lo+1:mid+1], fds)
    require_held(held_tap, held_frames, held_identities, [(i, raw) for i, raw in emitted if lo < i <= mid])
    tap, observed = tap_evidence(events[lo+1:hi+1], fds)
    assert [key for key in tap if key[0] == 'rx'] == [key for key in held_tap if key[0] == 'rx'], 'RX advanced after held sample'
    assert not (collections.Counter(tap)-collections.Counter(packet_frames)), 'TAP capture mismatch'
    interval = [r for seq, r in sorted(frames.items()) if start['FrameSequence'] < seq <= terminal['FrameSequence']]
    traced_raw = [raw for i, raw in emitted if lo < i <= hi]
    assert [base64.b64decode(r['producer']['raw']) for r in interval] == traced_raw, 'missing/intervening frame mismatch'
    for raw in traced_raw:
        metrics = json.loads(raw)['metrics']
        assert metrics['mmds']['rx_accepted'] == 0, 'MMDS outside measured RX-only window'
        assert metrics['net']['tap_read_fails'] == 0 and metrics['net']['tap_write_fails'] == 0
    measured = {'rx': sum(int(r['deltaRxBytes']) for r in interval), 'tx': sum(int(r['deltaTxBytes']) for r in interval)}
    classified = [{'frame': key, 'identity': identities[key]} for key in tap]
    outside = collections.Counter(packet_frames)-collections.Counter(tap)
    result = {'passed': False, 'start': start, 'held': held, 'terminal': terminal, 'observed': observed, 'measured': measured, 'calibrated_frames': classified, 'outside_device_window': [{'frame': k, 'identity': identities[k]} for k in outside.elements()], 'trace_hashes': trace_hashes, 'raw_reopen_sha256': raw_hashes, 'observed_tap_prefix_bytes': 12, 'prefix_interpretation': 'exact AF_PACKET suffix match; pinned virtio_net_hdr_v1 interpretation, not ioctl negotiation readback', 'deferred_interpretation': 'pinned-source inference, not direct descriptor inspection', 'complete': False, 'open_gates': ['unsent TX', 'MMDS', 'transport loss', 'cloud/provider billing', 'run5 unexplained kernel panic and general reliability']}
    save(base/'deferred-rx-result.json', result)
    assert observed == measured, 'exact byte mismatch'
    assert measured['rx'] == 2*(12+14+20+8+64), 'B must count; C must not count'
    assert sorted(identities[k]['sequence'] for k in outside.elements() if identities[k]['kind'] == 'controlled_icmp') == [2, 3], 'unexpected outside-window controlled traffic'
    assert [identities[k]['sequence'] for k in tap if k[0] == 'tx' and identities[k]['kind'] == 'controlled_icmp'] == [0]
    result['passed'] = True
    save(base/'deferred-rx-result.json', result)
    print('SUP921 exact deferred-RX terminal comparison passed')


def selftest_deferred():
    selftest()
    def frame(seq, reply=False):
        payload = struct.pack('!BBHHH', 0 if reply else 8, 0, 0, RX_IDENTIFIER, seq)+rx_payload(seq)
        payload = payload[:2]+struct.pack('!H', checksum(payload))+payload[4:]
        src, dst = ('169.254.0.21', '169.254.0.22') if reply else ('169.254.0.22', '169.254.0.21')
        ip = struct.pack('!BBHHHBBH4s4s', 0x45, 0, 92, 0, 0, 64, 1, 0, socket.inet_aton(src), socket.inet_aton(dst))
        ip = ip[:10]+struct.pack('!H', checksum(ip))+ip[12:]
        return b'\0'*12+b'\x08\x00'+ip+payload
    raw = [frame(i) for i in range(3)]+[frame(0, True)]
    packet = {'frames': [{'type': 4 if i < 3 else 0, 'hex': r.hex(), 'bytes': len(r), 'sha256': sha(r)} for i, r in enumerate(raw)], 'capture_errors': [], 'replies': [0]}
    frames, identities = packet_evidence(packet)
    tap = [frames[0], frames[1], frames[3]]
    metric = {'metrics': {'net': {'rx_rate_limiter_throttled': 1, 'tap_read_fails': 0, 'tap_write_fails': 0, 'no_rx_avail_buffer': 0}, 'mmds': {'rx_accepted': 0}}}
    emitted = [(1, json.dumps(metric).encode())]
    require_held(tap, frames, identities, emitted)
    rejected = 0
    for bad in (tap+[frames[2]], [frames[0], frames[3]], [frames[1], frames[0], frames[3]], tap+[frames[3]]):
        try: require_held(bad, frames, identities, emitted)
        except AssertionError: rejected += 1
        else: raise AssertionError('accepted invalid held state')
    metric['metrics']['net']['rx_rate_limiter_throttled'] = 0
    try: require_held(tap, frames, identities, [(1, json.dumps(metric).encode())])
    except AssertionError: rejected += 1
    else: raise AssertionError('accepted unthrottled state')
    try: rx_identify(raw[0][:-1]+b'z', 'rx')
    except AssertionError: rejected += 1
    else: raise AssertionError('accepted changed packet')
    assert rejected == 6
    print('SUP921 packet identity and six held-state rejection cases passed')
    # Exercise the final on-disk verifier with real string-encoded journal
    # counters, completed FIFO writes, exact fences and independent packets.
    def fixture(base):
        directory = base/'calibration-deferred-rx'; directory.mkdir()
        (base/'spool').mkdir()
        save(directory/'fds.json', {'tap': '14', 'metrics': '15'})
        save(directory/'limiter.json', {'method': 'PATCH', 'path': '/network-interfaces/eth0', 'request': {'iface_id': 'eth0', 'rx_rate_limiter': {'operations': {'size': 1, 'one_time_burst': 0, 'refill_time': 3600000}}}, 'status': 204, 'response': ''})
        metric['metrics']['net']['rx_rate_limiter_throttled'] = 1
        records = []; producer_raw = []
        for index, name in enumerate(('start', 'held', 'terminal')):
            header = {'schema': 'sig.fc-measurement.v1', 'incarnation': 'fixture', 'attempt_sequence': index+1, 'request_sequence': index+1, 'request_id': name, 'loss_detected': False}
            value = {'measurement': header, **metric}
            if name == 'terminal': value['terminal_cutoff'] = 'last_completed_device_operation'
            frame_raw = (json.dumps(value)+'\n').encode(); producer_raw.append(frame_raw)
            sequence = index*2+1
            fence = {'Scope': 'terminal_device_cutoff' if name == 'terminal' else 'device_sample', 'Request': {'request_id': name}, 'Header': header, 'FrameSequence': sequence, 'CorrelationSequence': sequence+1, 'FrameSHA256': sha(frame_raw), 'FrameBytes': len(frame_raw), 'Cutoff': 'last_completed_device_operation' if name == 'terminal' else ''}
            save(directory/(name+'.json'), fence)
            records.extend([{'sequence': str(sequence), 'kind': 'producer_frame', 'valid': True, 'complete': False, 'deltaRxBytes': '236' if name == 'held' else '0', 'deltaTxBytes': '118' if name == 'held' else '0', 'producer': {'incarnation': 'fixture', 'raw': base64.b64encode(frame_raw).decode()}}, {'sequence': str(sequence+1), 'kind': 'producer_correlation', 'valid': True, 'complete': False, 'producer': {'frameSha256': sha(frame_raw)}}])
        (base/'spool'/'fixture.jsonl').write_text(''.join(json.dumps(r)+'\n' for r in records))
        def call(op, fd, data):
            encoded = ''.join('\\x%02x' % byte for byte in data)
            return f'{op}({fd}, "{encoded}", {len(data)}) = {len(data)}\n'
        trace = call('write', 15, producer_raw[0])+call('read', 14, b'\0'*12+raw[0])+call('write', 14, b'\0'*12+raw[3])+call('read', 14, b'\0'*12+raw[1])+call('write', 15, producer_raw[1])+call('write', 15, producer_raw[2])
        (directory/'trace.1').write_text(trace)
        held_packet = {**packet, 'sends': [0, 1, 2]}
        save(directory/'held-packets.json', held_packet)
        fourth = frame(3)
        final_packet = {**held_packet, 'sends': [0, 1, 2, 3], 'frames': packet['frames']+[{'type': 4, 'hex': fourth.hex(), 'bytes': len(fourth), 'sha256': sha(fourth)}], 'kernel_drops': 0, 'kernel_packets': 5, 'elapsed_seconds': 2}
        save(directory/'packets.json', final_packet)
        return directory

    def change_json(path, mutate):
        value = json.loads(path.read_text()); mutate(value); save(path, value)

    mutations = [
        lambda d: change_json(d/'packets.json', lambda v: v.update(kernel_drops=1)),
        lambda d: change_json(d/'packets.json', lambda v: v.update(kernel_packets=6)),
        lambda d: change_json(d/'terminal.json', lambda v: v.update(FrameSHA256='0'*64)),
        lambda d: change_json(d/'held.json', lambda v: v.update(Header={**v['Header'], 'incarnation': 'other'})),
        lambda d: (d/'trace.1').write_text((d/'trace.1').read_text()+'read(14, <unfinished ...>\n'),
        lambda d: (d/'trace.1').write_text((d/'trace.1').read_text()+'read(14, "\\x00", 1) = 1\n'),
        lambda d: (d.parent/'spool'/'fixture.jsonl').write_text((d.parent/'spool'/'fixture.jsonl').read_text().replace('"236"', '"235"')),
        lambda d: (d.parent/'spool'/'fixture.jsonl').write_text((d.parent/'spool'/'fixture.jsonl').read_text().rstrip('\n')),
    ]
    for mutate in [None]+mutations:
        with tempfile.TemporaryDirectory(prefix='sup921-parser-') as temp:
            base = pathlib.Path(temp).resolve()
            assert base.parent == pathlib.Path(tempfile.gettempdir()).resolve() and base.name.startswith('sup921-parser-')
            directory = fixture(base)
            if mutate: mutate(directory)
            try: verify_deferred(base)
            except (AssertionError, KeyError):
                if not mutate: raise
            else:
                assert mutate is None, 'final verifier accepted invalid evidence'
    print('SUP921 final disk/fence verifier: positive and eight negative cases passed')


if __name__ == '__main__':
    if sys.argv[1] == 'capture': deferred_capture(sys.argv[2])
    elif sys.argv[1] == 'held': held_check(sys.argv[2])
    elif sys.argv[1] == 'verify-deferred': verify_deferred(sys.argv[2])
    elif sys.argv[1] == 'selftest-deferred': selftest_deferred()
    else: raise ValueError('unknown deferred RX command')
