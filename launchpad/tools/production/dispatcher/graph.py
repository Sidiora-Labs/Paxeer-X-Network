"""Bounded, fail-closed scheduling projection of the normative TOML task DAG."""
import copy
import fnmatch
import hashlib
import json
from pathlib import Path
import re
import tomllib
from collections import Counter, deque

class GraphError(ValueError):
    pass

_STATUSES = frozenset(('pending', 'in_progress', 'implemented', 'done', 'blocked', 'failed', 'paused'))

def _flatten(node, prefix=''):
    if not isinstance(node, dict):
        raise GraphError('task tree must contain tables')
    result = {}
    for key, value in node.items():
        identifier = f'{prefix}.{key}' if prefix else key
        if not isinstance(value, dict):
            raise GraphError(f'invalid task table {identifier}')
        if 'title' in value:
            if not re.fullmatch(r'\d+\.\d+', identifier):
                raise GraphError(f'invalid task identifier {identifier}')
            result[identifier] = copy.deepcopy(value)
        else:
            result.update(_flatten(value, identifier))
    return result

def _safe_path(pattern):
    return isinstance(pattern, str) and bool(pattern) and not pattern.startswith('/') and '\\' not in pattern and all(p not in ('', '.', '..') for p in pattern.split('/'))

def _owns(path, patterns):
    return any(path == p or fnmatch.fnmatchcase(path, p) or (p.endswith('/**') and path.startswith(p[:-3] + '/')) for p in patterns)

def _overlap(a, b):
    pa, pb = (re.split(r'[?*\[]', x, maxsplit=1)[0].rstrip('/') for x in (a,b))
    if a == b or fnmatch.fnmatchcase(a,b) or fnmatch.fnmatchcase(b,a):
        return True
    if a.endswith('/**') and (pb == pa or pb.startswith(pa+'/')):
        return True
    if b.endswith('/**') and (pa == pb or pa.startswith(pb+'/')):
        return True
    return pa != a and pb != b and bool(pa and pb and (pa == pb or pa.startswith(pb+'/') or pb.startswith(pa+'/')))

class Graph:
    def __init__(self, spec_path, plan_path=None):
        try:
            raw = Path(spec_path).read_bytes()
            data = tomllib.loads(raw.decode('utf-8'))
            self.tasks = _flatten(data.get('task', {}))
            if not self.tasks or len(self.tasks) > 10000:
                raise GraphError('task count outside bounded limits')
            lanes = data.get('lane', {})
            lane_patterns = []
            for lane, definition in lanes.items():
                patterns = definition.get('write_paths', [])
                if not patterns or any(not _safe_path(p) for p in patterns):
                    raise GraphError(f'invalid ownership paths for {lane}')
                reserved = data.get('execution', {}).get('reserved', {}).get('paths', [])
                for pattern in patterns:
                    if any(pattern == item or fnmatch.fnmatchcase(pattern, item) or _owns(item, [pattern]) for item in reserved):
                        raise GraphError(f'reserved ownership path {lane}:{pattern}')
                lane_patterns.extend((lane,p) for p in patterns)
            for index,(lane, pattern) in enumerate(lane_patterns):
                for other, candidate in lane_patterns[index+1:]:
                    if other != lane and _overlap(pattern,candidate):
                        raise GraphError(f'ownership overlap {lane}:{pattern} {other}:{candidate}')
            self._dependencies = {}
            successors = {identifier: [] for identifier in self.tasks}
            indegrees = {}
            for identifier, task in self.tasks.items():
                wave = task.get('wave')
                if type(wave) is not int or wave < 0 or task.get('status') not in _STATUSES:
                    raise GraphError(f'invalid wave/status {identifier}')
                requires = task.get('requires', [])
                if not isinstance(requires,list) or any(not isinstance(x,str) for x in requires) or len(set(requires)) != len(requires):
                    raise GraphError(f'invalid dependencies {identifier}')
                self._dependencies[identifier] = tuple(requires)
                indegrees[identifier] = len(requires)
                lane = task.get('lane')
                if task.get('kind') == 'parallel_lane':
                    if lane not in lanes or task.get('phase') not in (1,2,3,4) or wave != task['phase']*2-1:
                        raise GraphError(f'invalid parallel lane/wave {identifier}')
                    if not task.get('touches') or any(not _safe_path(p) or not _owns(p,lanes[lane]['write_paths']) for p in task['touches']):
                        raise GraphError(f'unowned touch {identifier}')
                for dep in requires:
                    if dep not in self.tasks:
                        raise GraphError(f'unknown dependency {identifier}:{dep}')
                    previous = self.tasks[dep].get('wave')
                    if type(previous) is not int or not (previous < wave or wave == previous == 0 and task.get('kind') != 'parallel_lane'):
                        raise GraphError(f'dependency wave violation {identifier}:{dep}')
                    successors[dep].append(identifier)
            queue = deque(identifier for identifier,n in indegrees.items() if n == 0)
            self._topology = []
            while queue:
                identifier = queue.popleft()
                self._topology.append(identifier)
                for successor in successors[identifier]:
                    indegrees[successor] -= 1
                    if not indegrees[successor]:
                        queue.append(successor)
            if len(self._topology) != len(self.tasks):
                raise GraphError('dependency cycle')
            if plan_path is not None:
                plan = json.loads(Path(plan_path).read_text())
                if plan.get('spec_sha256') != hashlib.sha256(raw).hexdigest() or plan.get('tasks') != self.tasks:
                    raise GraphError('stale or divergent execution plan projection')
                projected = {item['id']: item for item in plan.get('lanes', [])}
                if set(projected) != set(lanes):
                    raise GraphError('divergent lane projection')
                for lane, definition in lanes.items():
                    entry = projected[lane]
                    if entry.get('write_paths') != definition['write_paths']:
                        raise GraphError('divergent lane ownership projection')
                    expected = [dict(id=identifier,**task) for identifier,task in self.tasks.items() if task.get('lane') == lane]
                    if entry.get('tasks') != expected:
                        raise GraphError('divergent lane task projection')
            layout = {'tasks': {k:{a:v for a,v in t.items() if a != 'status'} for k,t in self.tasks.items()}, 'lanes': {k:v['write_paths'] for k,v in lanes.items()}}
            self.layout_digest = hashlib.sha256(json.dumps(layout,sort_keys=True,separators=(',',':')).encode()).hexdigest()
        except (OSError,UnicodeError,tomllib.TOMLDecodeError,json.JSONDecodeError,KeyError,TypeError) as exc:
            raise GraphError(f'invalid graph input: {exc}') from exc

    def ready(self, statuses=None):
        current = {k:t['status'] for k,t in self.tasks.items()}
        if statuses is not None:
            if not isinstance(statuses,dict) or any(k not in current or (not isinstance(v,str) or v not in _STATUSES) for k,v in statuses.items()):
                raise GraphError('invalid isolated status overlay')
            current.update(statuses)
        eligible = [k for k in self._topology if current[k] == 'pending' and all(current[d] == 'done' for d in self._dependencies[k])]
        if not eligible:
            return []
        wave = min(self.tasks[k]['wave'] for k in eligible)
        return sorted(k for k in eligible if self.tasks[k]['wave'] == wave)

    def status(self):
        return {'task_count':len(self.tasks),'edge_count':sum(map(len,self._dependencies.values())), 'counts':dict(sorted(Counter(t['status'] for t in self.tasks.values()).items())), 'ready':self.ready(), 'layout_digest':self.layout_digest}
