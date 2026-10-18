#!/usr/bin/env python3
"""Project spec/release-v6-12-0/spec.kvx into the two Hyperfide KVX generations.

Usage: hyperfide_projection.py PROFILE.json [--allow-placeholders] [--check]

Generation 1 imports every task that can be imported; generation 2 is the amendment
revision that omits the done tasks (Hyperfide retires them) and the held tasks.
Every rule below names the Hyperfide source line it follows (services/hyperfide/src).
"""
import argparse
import hashlib
import json
import re
import sys
from pathlib import Path

SPEC_SLUG = 'release-v6-12-0'
ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / 'spec' / SPEC_SLUG / 'spec.kvx'
OUT = ROOT / 'spec' / SPEC_SLUG / 'hyperfide'
ARTIFACT_ROOT = 'out/hyperfide/' + SPEC_SLUG  # /out/ is ignored by the repository .gitignore
CONNECTOR = 'claude-code'  # R4; packets.ts:283 accepts codex|claude-code
HELD_FEATURE = '6'
HELD_PREFIX = 'Hyperpax:'

# spec/import.ts:54-100 (parseKvx), copied exactly.
SECTION_RE = re.compile(r'^\[[A-Za-z0-9_][A-Za-z0-9_.:-]{0,254}\]$')
FIELD_RE = re.compile(r'^([A-Za-z_][A-Za-z0-9_.-]*)\s*=\s*(.+)$')
JSON_START_RE = re.compile(r'^(?:["[{]|true$|false$|null$|-?[0-9])')
BARE_RE = re.compile(r'^[A-Za-z_][A-Za-z0-9_.:/{}$-]*$')
# JS '.' never matches these line terminators; Python's does, so they are refused explicitly.
JS_LINE_TERMINATORS = ('\r', '\u2028', '\u2029')
UUID_RE = re.compile(r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')
SHA_RE = re.compile(r'^sha256:[0-9a-f]{64}$')
CLAUSE_RE = re.compile(r'^[0-9]+(?:\.[0-9]+)+$')  # obligations.ts:122
REPO_PREFIX_RE = re.compile(r'^[A-Za-z][A-Za-z0-9_.-]*:')
# R3: a verify or build that reads a local env file needs live infrastructure.
LIVE_RE = re.compile(r'--env-file\b|(?:^|[\s=/])\.env(?:\.[A-Za-z0-9_-]+)?(?=\s|$)')
PLACEHOLDER_UUID = '00000000-0000-0000-0000-'
PLACEHOLDER_TEXT = 'placeholder:'
MAX_SAFE = 2 ** 53 - 1
IPV4_RE = re.compile(r'\b(?:\d{1,3}\.){3}\d{1,3}\b')
DATE_RE = re.compile(r'\b20\d\d-[01]\d-[0-3]\d\b')


class SpecError(Exception):
    pass


def strip_comment(line):
    quoted = escaped = False
    for i, c in enumerate(line):
        if escaped:
            escaped = False
            continue
        if c == '\\' and quoted:
            escaped = True
            continue
        if c == '"':
            quoted = not quoted
        if c == '#' and not quoted:
            return line[:i].strip()
    return line.strip()


def safe_numbers(value, where):
    if isinstance(value, bool):
        return
    if isinstance(value, float) and (value != value or value in (float('inf'), float('-inf'))):
        raise SpecError('non-finite number at ' + where)
    if isinstance(value, int) and abs(value) > MAX_SAFE:
        raise SpecError('unsafe integer at ' + where)
    if isinstance(value, list):
        for item in value:
            safe_numbers(item, where)
    elif isinstance(value, dict):
        for item in value.values():
            safe_numbers(item, where)


def parse_kvx(text):
    """Returns [(name, {key: value})] in source order, enforcing parseKvx exactly."""
    if '\0' in text:
        raise SpecError('NUL byte')
    sections, seen, current = [], set(), None
    for index, line in enumerate(text.split('\n'), 1):
        body = strip_comment(line)
        if not body:
            continue
        if any(t in body for t in JS_LINE_TERMINATORS):
            raise SpecError('JS line terminator at line %d' % index)
        if body.startswith('['):
            if not SECTION_RE.match(body):
                raise SpecError('malformed section at line %d' % index)
            name = body[1:-1]
            if name in seen:
                raise SpecError('duplicate section at line %d' % index)
            seen.add(name)
            current = (name, {})
            sections.append(current)
            continue
        match = FIELD_RE.match(body)
        if current is None or not match:
            raise SpecError('malformed field at line %d' % index)
        key, raw = match.group(1), match.group(2).strip()
        if key in current[1]:
            raise SpecError('duplicate key at line %d' % index)
        if JSON_START_RE.match(raw):
            try:
                value = json.loads(raw)
            except ValueError:
                raise SpecError('malformed value at line %d' % index)
        elif BARE_RE.match(raw):
            value = raw
        else:
            raise SpecError('malformed value at line %d' % index)
        safe_numbers(value, 'line %d' % index)
        current[1][key] = value
    if not sections or not any(n.startswith('task.') for n, _ in sections):
        raise SpecError('KVX requires nonempty sections and task definitions')
    return sections


def canonical(value):
    """spec/import.ts:43-47: sorted keys, JSON.stringify scalars."""
    if isinstance(value, list):
        return '[' + ','.join(canonical(v) for v in value) + ']'
    if isinstance(value, dict):
        return '{' + ','.join(json.dumps(k, ensure_ascii=False) + ':' + canonical(value[k]) for k in sorted(value)) + '}'
    return json.dumps(value, ensure_ascii=False)


def digest(text):
    return 'sha256:' + hashlib.sha256(text.encode('utf-8')).hexdigest()


def emit_value(value):
    text = json.dumps(value, ensure_ascii=False)
    return text.replace('\u2028', '\\u2028').replace('\u2029', '\\u2029')


def safe_touch(value):
    """dependencies.ts:57-60."""
    return (isinstance(value, str) and 0 < len(value) <= 1024 and not value.startswith('/')
            and not re.search(r'[\\\x00-\x1f]', value)
            and all(p not in ('', '.', '..') for p in value.split('/')))


def safe_artifact(value):
    """obligations.ts:42-45."""
    return (len(value) <= 1024 and not value.startswith('/') and '\\' not in value
            and not re.search(r'[\x00-\x1f]', value)
            and all(p not in ('', '.', '..') for p in value.split('/')))


def packet_text(value, limit=255):
    """hierarchy/packages.ts:70-74."""
    return isinstance(value, str) and 1 <= len(value) <= limit and not re.search(r'[\x00-\x1f\x7f]', value)


def feature_of(name, fields, features, slug):
    """spec/import.ts:187-189."""
    key = name[5:]
    if isinstance(fields.get('feature'), str):
        return fields['feature']
    prefix = key.split('.')[0]
    return prefix if prefix in features else 'programme:' + slug


# ---------------------------------------------------------------- profile

PROFILE_KEYS = {'environment_profile_id', 'resource_requirements', 'execution_profile', 'max_process_lifetime_seconds',
                'review', 'graph_inputs'}


def load_profile(path, allow_placeholders):
    profile = json.loads(Path(path).read_text())
    profile = {k: v for k, v in profile.items() if not k.startswith('_')}
    if set(profile) != PROFILE_KEYS:
        raise SpecError('profile keys must be exactly ' + ', '.join(sorted(PROFILE_KEYS)))
    rr = profile['resource_requirements']
    if set(rr) != {'cpu_millis', 'memory_bytes', 'disk_bytes', 'build_slots', 'test_slots', 'max_descendants'}:
        raise SpecError('resource_requirements keys differ from packets.ts:79')
    if not isinstance(rr['cpu_millis'], int) or rr['cpu_millis'] < 1:
        raise SpecError('cpu_millis must be a positive integer (packets.ts:80)')
    for k in ('build_slots', 'test_slots', 'max_descendants'):
        if not isinstance(rr[k], int) or isinstance(rr[k], bool) or rr[k] < 0:
            raise SpecError(k + ' must be a nonnegative integer (packets.ts:81)')
    for k in ('memory_bytes', 'disk_bytes'):
        if not isinstance(rr[k], str) or not re.match(r'^[1-9][0-9]*$', rr[k]) or int(rr[k]) > 9223372036854775807:
            raise SpecError(k + ' must be a nonzero decimal string (packages.ts:80-83)')
    ep = profile['execution_profile']
    if set(ep) != {'model_profile_ref', 'credential_profile_ref'} or not all(packet_text(ep[k]) for k in ep):
        raise SpecError('execution_profile needs model_profile_ref and credential_profile_ref text (packets.ts:284)')
    life = profile['max_process_lifetime_seconds']
    if not isinstance(life, int) or life < 1:
        raise SpecError('max_process_lifetime_seconds must be a positive integer')
    reviewers = profile['review'].get('reviewer_ids') if isinstance(profile['review'], dict) else None
    if set(profile['review']) != {'reviewer_ids'} or not isinstance(reviewers, list) or not reviewers or \
            len(set(reviewers)) != len(reviewers) or not all(isinstance(r, str) and UUID_RE.match(r) for r in reviewers):
        raise SpecError('review.reviewer_ids must be unique UUIDs (obligations.ts:87)')
    gi = profile['graph_inputs']
    if set(gi) != {'required', 'manifest_id', 'reason'} or not isinstance(gi['required'], bool):
        raise SpecError('graph_inputs keys differ from packets.ts:390')
    if gi['required'] and (not isinstance(gi['manifest_id'], str) or not UUID_RE.match(gi['manifest_id']) or gi['reason'] is not None):
        raise SpecError('required graph needs a manifest UUID and a null reason (packets.ts:394-396)')
    if not gi['required'] and (gi['manifest_id'] is not None or not packet_text(gi['reason'], 4096)):
        raise SpecError('non-required graph needs a null manifest and a reason (packets.ts:402-404)')
    if not isinstance(profile['environment_profile_id'], str) or not UUID_RE.match(profile['environment_profile_id']):
        raise SpecError('environment_profile_id must be a UUID (packets.ts:364)')
    placeholders = [v for v in strings_in(profile) if v.startswith(PLACEHOLDER_UUID) or v.startswith(PLACEHOLDER_TEXT)]
    if placeholders and not allow_placeholders:
        raise SpecError('profile carries placeholder values (%d); pass --allow-placeholders only for a dry run' % len(placeholders))
    return profile, bool(placeholders)


def strings_in(value):
    if isinstance(value, str):
        yield value
    elif isinstance(value, list):
        for v in value:
            yield from strings_in(v)
    elif isinstance(value, dict):
        for v in value.values():
            yield from strings_in(v)


# ---------------------------------------------------------------- projection

def project(source_text, profile):
    sections = parse_kvx(source_text)
    by_name = dict(sections)
    features = {n[8:] for n, _ in sections if n.startswith('feature.')}
    slug = str(by_name.get('meta', {}).get('slug', 'programme'))
    tasks = {n[5:]: f for n, f in sections if n.startswith('task.')}
    feature = {k: feature_of('task.' + k, f, features, slug) for k, f in tasks.items()}
    done = {k for k, f in tasks.items() if f.get('status') == 'done'}
    unknown = sorted(r for f in tasks.values() for r in f.get('requires', []) if not isinstance(r, str) or r not in tasks)
    if unknown:
        raise SpecError('requires names absent tasks: ' + ', '.join(map(str, unknown)))

    held = {}
    for k, f in tasks.items():
        if k in done:
            continue
        prefixed = [t for t in f['touches'] if REPO_PREFIX_RE.match(t)]
        if feature[k] == HELD_FEATURE and any(t.startswith(HELD_PREFIX) for t in prefixed):
            held[k] = 'R2: feature 6 task touching Hyperpax: paths'
        elif prefixed:
            held[k] = 'touches another repository (%s); one proof policy binds one repository (obligations.ts:18-19)' % prefixed[0].split(':')[0]
    changed = True
    while changed:  # a pending dependent of a held task cannot resolve its edge in either generation
        changed = False
        for k, f in tasks.items():
            if k in done or k in held:
                continue
            blocking = [r for r in f['requires'] if r in held]
            if blocking:
                held[k] = 'requires held task %s; the edge cannot resolve without it (dependencies.ts:183-190)' % blocking[0]
                changed = True

    review = {k for k, f in tasks.items() if k not in held and (LIVE_RE.search(f['verify_cmd']) or LIVE_RE.search(f['build_cmd']))}

    gen1 = [k for k in tasks if k not in held]
    gen2 = [k for k in gen1 if k not in done]
    stats = {'cross_feature': {1: 0, 2: 0}, 'dropped_done_edges': [], 'dropped_held_edges': []}

    def requires_for(k, generation):
        out = []
        for r in tasks[k]['requires']:
            if r in held:
                stats['dropped_held_edges'].append((k, r))  # only done tasks reach here
                continue
            if generation == 2 and r in done:
                stats['dropped_done_edges'].append((k, r))
                continue
            if feature[r] == feature[k]:
                out.append(r)
            else:
                out.append({'feature_key': feature[r], 'task_key': r})
                stats['cross_feature'][generation] += 1
        return out

    rubric_sections = review_sections(tasks, by_name, review)
    kept = [(n, f) for n, f in sections
            if n == 'meta' or n.startswith(('feature.', 'req.', 'acceptance.'))]
    globals_ = kept + rubric_sections

    def task_section(k, generation):
        f = tasks[k]
        command = folded_command(f)
        artifact = '%s/%s/evidence-index.json' % (ARTIFACT_ROOT, k)
        index = canonical({'schema_version': 1, 'spec': SPEC_SLUG, 'task_key': k, 'reqs': f['reqs'],
                           'command_sha256': digest(command)})
        writer = "mkdir -p %s/%s && printf '%%s\\n' '%s' > %s" % (ARTIFACT_ROOT, k, index, artifact)
        timeout = int(f['time_limit_minutes']) * 60
        if timeout > profile['max_process_lifetime_seconds']:
            raise SpecError('task %s timeout %ds exceeds the profile lifetime (packets.ts:286)' % (k, timeout))
        fields = {}
        for key, value in f.items():
            if key == 'requires':
                fields[key] = requires_for(k, generation)
            elif key == 'wave':
                # import.ts:183,190 add 1 to every wave only while some task has wave 0; gen2 has none,
                # so both generations declare wave+1 to keep the runtime wave and package key
                # (revisions.ts:99 refuses relocation).
                fields['wave'] = value + 1
                fields['source_wave'] = value
            elif key == 'verify_cmd':
                fields[key] = command + ' && ' + writer
            else:
                fields[key] = value
        fields['artifacts'] = [artifact]
        fields['environment_profile_id'] = profile['environment_profile_id']
        fields['resource_requirements'] = dict(agent_sessions=1, **profile['resource_requirements'])
        fields['execution_profile'] = {'connector': CONNECTOR,
                                       'model_profile_ref': profile['execution_profile']['model_profile_ref'],
                                       'credential_profile_ref': profile['execution_profile']['credential_profile_ref'],
                                       'timeout_seconds': timeout}
        fields['contract_inputs'] = []
        fields['graph_inputs'] = dict(profile['graph_inputs'])
        fields['proof_mode'] = 'REVIEW' if k in review else 'EXECUTABLE'
        return ('task.' + k, fields)

    doc1 = globals_ + [task_section(k, 1) for k in gen1]
    doc2 = globals_ + [task_section(k, 2) for k in gen2]
    return {'tasks': tasks, 'feature': feature, 'done': done, 'held': held, 'review': review,
            'gen1': gen1, 'gen2': gen2, 'doc1': doc1, 'doc2': doc2, 'stats': stats, 'by_name': by_name}


def folded_command(f):
    build, verify = f['build_cmd'].strip(), f['verify_cmd'].strip()
    # Subshells keep the declared commands' own cwd and shell state apart, as when they ran separately.
    if build and build != verify:
        return '( %s ) && ( %s )' % (build, verify)
    return '( %s )' % verify


def review_sections(tasks, by_name, review):
    """REVIEW clauses need accepted [proof.review.<clause>] (obligations.ts:188-192); a task whose
    clauses are all REVIEW has no checks, so its rubric must match [proof] (obligations.ts:162-164)."""
    if not review:
        return []
    commands = {}
    for k in sorted(review):
        for clause in tasks[k]['reqs']:
            commands.setdefault(clause, set()).add(tasks[k]['verify_cmd'])
    out = []
    reviewers = REVIEWERS[0]
    programme = ('Review-only task proof for %s: every clause of a task whose verify_cmd needs a live fleet or a local '
                 'environment file is closed by the judgement of its [proof.review.<clause>] rubric.' % SPEC_SLUG)
    out.append(('proof', {'rubric': programme, 'rubric_digest': digest(programme)}))
    for clause in sorted(commands, key=lambda c: [int(p) for p in c.split('.')]):
        split = clause.rindex('.')
        criterion = by_name.get('req.' + clause[:split], {}).get('ac_' + clause[split + 1:])
        rubric = canonical({'schema_version': 1, 'spec': SPEC_SLUG, 'clause': clause, 'criterion': criterion,
                            'evidence_commands': sorted(commands[clause]),
                            'judgement': 'PASS only when the receipts written by the evidence commands against the live '
                                         'fleet show the criterion holds; attach the receipts as support artifacts.'})
        out.append(('proof.review.' + clause, {'rubric': rubric, 'rubric_digest': digest(rubric), 'reviewers': reviewers}))
    return out


REVIEWERS = [None]


def render(doc):
    lines = []
    for name, fields in doc:
        lines.append('[%s]' % name)
        for key, value in fields.items():
            lines.append('%s = %s' % (key, emit_value(value)))
        lines.append('')
    return '\n'.join(lines)


# ---------------------------------------------------------------- self-check

def check_generation(label, text, generation_keys, done, held, expect_plain):
    errors = []
    try:
        sections = parse_kvx(text)
    except SpecError as error:
        return ['%s: parse: %s' % (label, error)], None
    by_name = dict(sections)
    if 'hyperfide.projection' in by_name:
        errors.append(label + ': [hyperfide.projection] is refused (import.ts:223)')
    features = {n[8:] for n, _ in sections if n.startswith('feature.')}
    slug = str(by_name.get('meta', {}).get('slug', 'programme'))
    tasks = {n[5:]: f for n, f in sections if n.startswith('task.')}
    if list(tasks) != list(generation_keys):
        errors.append(label + ': task set differs from the projection')
    feature = {k: feature_of('task.' + k, f, features, slug) for k, f in tasks.items()}
    offset = 1 if any(f.get('wave') == 0 for f in tasks.values()) else 0
    wave = {k: max(1, f['wave'] + offset) if isinstance(f.get('wave'), int) else 1 for k, f in tasks.items()}
    acceptances = [(n, f) for n, f in sections if n.startswith(('acceptance.', 'case.'))]
    adjacency = {k: [] for k in tasks}
    for k, f in tasks.items():
        e = lambda msg: errors.append('%s: task %s: %s' % (label, k, msg))
        touches = f.get('touches')
        if not isinstance(touches, list) or not touches:
            e('touches empty (packets.ts:367-368)')
        else:
            if not all(safe_touch(t) for t in touches):
                e('touches not safeTouch-valid (dependencies.ts:144)')
            if not all(packet_text(t) for t in touches) or len(set(touches)) != len(touches) or len(touches) > 4096:
                e('touches outside packet strings bounds (packages.ts:97-100)')
            if expect_plain and any(REPO_PREFIX_RE.match(t) for t in touches):
                e('dispatchable task carries a repository-prefixed touch')
        requires = f.get('requires')
        if not isinstance(requires, list):
            e('requires is not a list (dependencies.ts:148)')
            requires = []
        seen = set()
        for r in requires:
            if isinstance(r, str):
                target, target_feature = r, feature[k]
                if r in tasks and feature[r] != feature[k]:
                    e('bare cross-feature requires %s (dependencies.ts:187)' % r)
                    continue
            elif isinstance(r, dict):
                if set(r) != {'feature_key', 'task_key'} or not all(isinstance(r[x], str) and 0 < len(r[x]) <= 255 for x in r):
                    e('requires descriptor invalid (dependencies.ts:158-165)')
                    continue
                target, target_feature = r['task_key'], r['feature_key']
                if target_feature == feature[k]:
                    e('same-feature requires written as an object')
            else:
                e('requires entry invalid (dependencies.ts:179)')
                continue
            if target not in tasks or feature[target] != target_feature:
                why = 'retired' if target in done else 'held' if target in held else 'absent'
                e('requires %s target %s does not resolve in this generation (dependencies.ts:183-190)' % (why, target))
                continue
            if target == k:
                e('requires itself')
            if target in seen:
                e('duplicate requires ' + target)
            seen.add(target)
            if wave[target] > wave[k]:
                e('requires later-wave task ' + target)
            adjacency[k].append(target)
        reqs = f.get('reqs')
        if not isinstance(reqs, list) or not reqs or len(reqs) > 128 or len(set(reqs)) != len(reqs) or \
                not all(isinstance(c, str) and CLAUSE_RE.match(c) for c in reqs):
            e('reqs invalid: %s clauses, the proof mapping allows 1-128 unique ids (obligations.ts:39, 122)' % (len(reqs) if isinstance(reqs, list) else 'no'))
            reqs = []
        for clause in reqs:
            split = clause.rindex('.')
            if not isinstance(by_name.get('req.' + clause[:split], {}).get('ac_' + clause[split + 1:]), str):
                e('clause %s has no [req.%s] ac_%s (obligations.ts:131)' % (clause, clause[:split], clause[split + 1:]))
            if not any(a.get('req') == clause or (isinstance(a.get('reqs'), list) and clause in a['reqs']) for _, a in acceptances):
                e('clause %s has no acceptance coverage (obligations.ts:133-150)' % clause)
        command, artifacts = f.get('verify_cmd'), f.get('artifacts')
        if not isinstance(command, str) or not command or len(command) > 8192:
            e('verify_cmd absent or longer than 8192 (obligations.ts:176)')
        if not isinstance(artifacts, list) or not artifacts or len(set(artifacts)) != len(artifacts) or \
                not all(isinstance(a, str) and a and safe_artifact(a) for a in artifacts):
            e('artifacts invalid (obligations.ts:176)')
        elif isinstance(command, str) and not command.endswith('> ' + artifacts[0]):
            e('verify_cmd does not write its declared artifact (executor.ts:206-207)')
        if not isinstance(f.get('environment_profile_id'), str) or not UUID_RE.match(f['environment_profile_id']):
            e('environment_profile_id invalid (packets.ts:364)')
        rr = f.get('resource_requirements')
        if not isinstance(rr, dict) or set(rr) != {'agent_sessions', 'cpu_millis', 'memory_bytes', 'disk_bytes', 'build_slots',
                                                   'test_slots', 'max_descendants'} or rr['agent_sessions'] != 1:
            e('resource_requirements invalid (packets.ts:78-84)')
        ep = f.get('execution_profile')
        if not isinstance(ep, dict) or set(ep) != {'connector', 'model_profile_ref', 'credential_profile_ref', 'timeout_seconds'} or \
                ep['connector'] != CONNECTOR or not isinstance(ep['timeout_seconds'], int) or ep['timeout_seconds'] < 1:
            e('execution_profile invalid (packets.ts:281-287)')
        if f.get('contract_inputs') != []:
            e('contract_inputs must be the explicit empty set (packets.ts:379-380)')
        gi = f.get('graph_inputs')
        if not isinstance(gi, dict) or set(gi) != {'required', 'manifest_id', 'reason'}:
            e('graph_inputs invalid (packets.ts:390)')
        if f.get('proof_mode') == 'REVIEW':
            for clause in reqs:
                section = by_name.get('proof.review.' + clause)
                if not section or not isinstance(section.get('reviewers'), list) or not section['reviewers'] or \
                        not all(isinstance(r, str) and UUID_RE.match(r) for r in section['reviewers']) or \
                        not isinstance(section.get('rubric_digest'), str) or not SHA_RE.match(section['rubric_digest']) or \
                        digest(section.get('rubric', '')) != section['rubric_digest']:
                    e('REVIEW clause %s lacks an accepted [proof.review] section (obligations.ts:188-192)' % clause)
            proof = by_name.get('proof', {})
            if not isinstance(proof.get('rubric_digest'), str) or not SHA_RE.match(proof['rubric_digest']):
                e('all-REVIEW task needs [proof] rubric_digest (obligations.ts:163-164)')
    # Cycles (dependencies.ts:295-306).
    state = {}
    for start in tasks:
        if start in state:
            continue
        stack = [(start, iter(adjacency[start]))]
        state[start] = 1
        while stack:
            node, it = stack[-1]
            nxt = next(it, None)
            if nxt is None:
                state[node] = 2
                stack.pop()
            elif state.get(nxt) == 1:
                path = [n for n, _ in stack]
                errors.append('%s: dependency cycle %s' % (label, ' -> '.join(path[path.index(nxt):] + [nxt])))
            elif nxt not in state:
                state[nxt] = 1
                stack.append((nxt, iter(adjacency[nxt])))
    # Same-wave overlapping scope must be sequenced across owners (dependencies.ts:233-251).
    closure = {}

    def ancestors(k):
        if k not in closure:
            closure[k] = set()
            for p in adjacency[k]:
                closure[k] |= {p} | ancestors(p)
        return closure[k]
    claims = {}
    for k, f in tasks.items():
        for t in set(f.get('touches') or []):
            if safe_touch(t):
                claims.setdefault(t, []).append(k)
    advisories = 0
    pairs = set()
    for path, holders in claims.items():
        parts = path.split('/')
        for i in range(len(parts)):
            for other in claims.get('/'.join(parts[:i + 1]), []):
                for holder in holders:
                    if other != holder:
                        pairs.add(tuple(sorted((other, holder))))
    for a, b in sorted(pairs):
        if wave[a] != wave[b] or a in ancestors(b) or b in ancestors(a):
            continue
        owner_a = tasks[a].get('owner') or 'feature:' + feature[a]
        owner_b = tasks[b].get('owner') or 'feature:' + feature[b]
        if owner_a != owner_b:
            errors.append('%s: unsequenced same-wave scope overlap %s / %s (dependencies.ts:248)' % (label, a, b))
        else:
            advisories += 1
    for pattern, what in ((IPV4_RE, 'IPv4 address'), (DATE_RE, 'calendar date')):
        if pattern.search(text):
            errors.append('%s: output contains a %s' % (label, what))
    return errors, {'sections': sections, 'tasks': tasks, 'advisories': advisories, 'pairs': len(pairs)}


def self_check(result, text1, text2):
    errors1, info1 = check_generation('gen1', text1, result['gen1'], result['done'], result['held'], False)
    errors2, info2 = check_generation('gen2', text2, result['gen2'], result['done'], result['held'], True)
    errors = errors1 + errors2
    if info1 and info2:
        if not set(info2['tasks']) <= set(info1['tasks']):
            errors.append('gen2 task keys are not a subset of gen1')
        if any(k in result['done'] or k in result['held'] for k in info2['tasks']):
            errors.append('gen2 still carries a done or held task')
        g1 = [s for s in info1['sections'] if not s[0].startswith('task.')]
        g2 = [s for s in info2['sections'] if not s[0].startswith('task.')]
        if canonical(g1) != canonical(g2):
            errors.append('non-task sections differ between generations (revisions.ts:91-93)')
    summary = {'errors': len(errors),
               'gen1_tasks': len(info1['tasks']) if info1 else None, 'gen2_tasks': len(info2['tasks']) if info2 else None,
               'gen1_shared_scope_advisories': info1['advisories'] if info1 else None,
               'gen2_shared_scope_advisories': info2['advisories'] if info2 else None}
    return errors, summary


# ---------------------------------------------------------------- report

def coverage_removed(result):
    """revisions.ts:106-110: a REPLAN may not drop a reqs clause that the prior generation covered."""
    before = {c for k in result['gen1'] for c in result['tasks'][k]['reqs']}
    after = {c for k in result['gen2'] for c in result['tasks'][k]['reqs']}
    return sorted(before - after, key=lambda c: [int(p) for p in c.split('.')])


def report(result, check, placeholders):
    tasks, feature = result['tasks'], result['feature']
    held, review = result['held'], result['review']
    removed = coverage_removed(result)

    def per_feature(keys):
        counts = {}
        for k in keys:
            counts[feature[k]] = counts.get(feature[k], 0) + 1
        return ', '.join('%s: %d' % (f, counts[f]) for f in sorted(counts, key=lambda x: (len(x), x)))
    lines = ['# Hyperfide projection of %s' % SPEC_SLUG, '',
             'Generated by `tools/spec/hyperfide_projection.py` from `spec/%s/spec.kvx`. Do not edit by hand.' % SPEC_SLUG, '',
             '## Counts', '',
             '| | gen1.kvx | gen2.kvx |', '|---|---|---|',
             '| tasks | %d | %d |' % (len(result['gen1']), len(result['gen2'])),
             '| done tasks (retired by gen2) | %d | 0 |' % len([k for k in result['gen1'] if k in result['done']]),
             '| pending tasks | %d | %d |' % (len([k for k in result['gen1'] if k not in result['done']]), len(result['gen2'])),
             '| REVIEW-clause tasks | %d | %d |' % (len([k for k in result['gen1'] if k in review]), len([k for k in result['gen2'] if k in review])),
             '| cross-feature requires rewritten as objects | %d | %d |' % (result['stats']['cross_feature'][1], result['stats']['cross_feature'][2]),
             '| requires on done tasks removed | 0 | %d |' % len(result['stats']['dropped_done_edges']),
             '', 'Tasks per feature, gen1: ' + per_feature(result['gen1']) + '.',
             '', 'Tasks per feature, gen2: ' + per_feature(result['gen2']) + '.',
             '', 'Source: %d tasks, %d done, %d pending. Held from both generations: %d.' % (
                 len(tasks), len(result['done']), len(tasks) - len(result['done']), len(held)), '',
             '## Held tasks (outside Hyperfide, in neither generation)', '',
             '| task | wave | reason |', '|---|---|---|']
    lines += ['| %s | %s | %s |' % (k, tasks[k]['wave'], held[k]) for k in held]
    dropped_held = sorted(set(result['stats']['dropped_held_edges']))
    if dropped_held:
        lines += ['', 'Done tasks in gen1 lose their edge on a held task (the done task is retired in gen2, which strips its requires anyway, revisions.ts:307): ' +
                  ', '.join('%s -> %s' % e for e in dropped_held) + '.']
    lines += ['', '## REVIEW-clause tasks (R3)', '',
              'Each clause of these tasks is a REVIEW clause; `[proof.review.<clause>]` carries the rubric, its digest and the reviewer ids from the profile, and `[proof]` carries the rubric for tasks with no executable check (obligations.ts:162-164, 187-192). Register them with `spec.proof.review.register` (obligations.ts:222, 259).', '',
              '| task | wave | reqs |', '|---|---|---|']
    lines += ['| %s | %s | %s |' % (k, tasks[k]['wave'], ', '.join(tasks[k]['reqs'])) for k in tasks if k in review]
    lines += ['', '## Tasks that cannot be made fully admissible', '']
    for k in [k for k in tasks if k in review and k not in result['done']]:
        lines.append('- %s: every clause is REVIEW, so its policy has no check (an unmapped check is refused, obligations.ts:90); '
                     'an empty check selection cannot produce a receipt (assurance/executor.ts:244) and acceptance needs a receipt '
                     '(assurance/coverage.ts:115, 178). Registration and dispatch are admissible; qualification is not.' % k)
    for k in [k for k in result['gen1'] if len(tasks[k]['reqs']) > 128]:
        lines.append('- %s: %d reqs clauses; a proof policy maps at most 128 (obligations.ts:39, 62-63, 122), so no policy can be registered and it can never qualify. Splitting the task is a spec change outside this projection.' % (k, len(tasks[k]['reqs'])))
    for error in (check[0] if check else []):
        if 'dependency cycle' in error:
            lines.append('- %s: the source spec itself carries this requires cycle among done wave 0 tasks, so the gen1 import fails DEPENDENCY_CYCLE (dependencies.ts:295-306). The projection does not rewrite the source; gen2 is unaffected because it retires them.' % error)
    for k in held:
        lines.append('- %s: held, %s.' % (k, held[k]))
    lines += ['', '## Rulings checked against the code', '',
              '- R1: `exempt_task_ids` has no effect on dependency admission (dependencies.ts:322-325), and amendment analysis validates the proposed graph alone (revisions.ts:166-178, dependencies.ts:97-98), so an edge on a retired task fails as DEPENDENCY_UNRESOLVED (dependencies.ts:183-190). gen2 therefore removes every requires edge on a done task: %d edges.' % len(result['stats']['dropped_done_edges']),
              '- Waves: the importer adds 1 to every wave only while some task has wave 0 (import.ts:183, 190), and every wave 0 task is done, so gen2 would shift all runtime waves and package keys and the amendment would refuse each task as TASK_RELOCATION_REQUIRES_NEW_IDENTITY (revisions.ts:99). Both generations declare `wave` = source wave + 1 (no wave 0) and keep `source_wave`; the runtime waves equal what the importer assigns to the source.',
              '- Coverage: gen2 drops %d reqs clauses that only done or held tasks carry, so a REPLAN amendment fails ACCEPTANCE_COVERAGE_CANNOT_BE_REMOVED (revisions.ts:103-110); propose gen2 as INTENT_CHANGE, which needs an OPERATOR (revisions.ts:270, 355).' % len(removed),
              '- Every changed or new gen2 task needs a proof policy at admission (revisions.ts:315-320).',
              '- verify_cmd = `( build_cmd ) && ( verify_cmd ) && <writer>`; the writer creates `%s/<task>/evidence-index.json` (task key, reqs and the command digest), which the check must leave behind (assurance/executor.ts:206-207, 270-279). `/out/` is ignored by the repository .gitignore, so a worker cannot commit it out of scope.' % ARTIFACT_ROOT,
              '- execution_profile.timeout_seconds = time_limit_minutes x 60; the check deadline timeout_ms is chosen at proof registration and is capped by HF_PROOF_MAX_CHECK_MS (obligations.ts:206-210, 221).',
              '- Non-task sections kept: [meta], [feature.*], [req.*], [acceptance.*] (import.ts:181-182, obligations.ts:125-150), plus the generated [proof] and [proof.review.*]. They are identical in both generations (revisions.ts:91-93).',
              '', '## Self-check', '']
    if placeholders:
        lines.append('Profile: placeholder values (dry run with --allow-placeholders).')
    if check is None:
        lines.append('Not run.')
    else:
        errors, summary = check
        lines.append('Command: `python3 tools/spec/hyperfide_projection.py spec/%s/hyperfide/profile.example.json --allow-placeholders --check`' % SPEC_SLUG)
        lines.append('')
        lines.append('Exit code: %d. Summary: %s.' % (1 if errors else 0, canonical(summary)))
        lines += ['- ' + e for e in errors[:50]]
        if removed:
            lines += ['', 'Clauses removed by gen2: ' + ', '.join(removed) + '.']
    return '\n'.join(lines) + '\n'


def main(argv):
    parser = argparse.ArgumentParser(description=__doc__.split('\n')[0])
    parser.add_argument('profile')
    parser.add_argument('--allow-placeholders', action='store_true')
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args(argv)
    try:
        profile, placeholders = load_profile(args.profile, args.allow_placeholders)
        REVIEWERS[0] = profile['review']['reviewer_ids']
        result = project(SOURCE.read_text(encoding='utf-8'), profile)
    except SpecError as error:
        print('hyperfide_projection: ' + str(error), file=sys.stderr)
        return 2
    text1, text2 = render(result['doc1']), render(result['doc2'])
    check = self_check(result, text1, text2) if args.check else None
    OUT.mkdir(parents=True, exist_ok=True)
    (OUT / 'gen1.kvx').write_text(text1, encoding='utf-8')
    (OUT / 'gen2.kvx').write_text(text2, encoding='utf-8')
    (OUT / 'REPORT.md').write_text(report(result, check, placeholders), encoding='utf-8')
    print('gen1 %d tasks, gen2 %d tasks, held %d, review %d' % (
        len(result['gen1']), len(result['gen2']), len(result['held']), len(result['review'])))
    if check is not None:
        errors, summary = check
        for error in errors:
            print(error, file=sys.stderr)
        print('self-check: ' + canonical(summary))
        return 1 if errors else 0
    return 0


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
