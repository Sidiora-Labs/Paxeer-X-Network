#!/usr/bin/env python3
import argparse
import datetime
import json
import pathlib


def breaking_changes(before, after):
    changes = []
    def compare(old, new, pointer):
        if not isinstance(old, dict) or not isinstance(new, dict):
            if old != new: changes.append(pointer + ': schema kind changed')
            return
        for key in ('$ref', 'type', 'format', 'pattern', 'contentEncoding'):
            if old.get(key) != new.get(key): changes.append(pointer + ': ' + key + ' changed')
        if 'enum' not in old and 'enum' in new: changes.append(pointer + ': enum introduced')
        if 'enum' in old and not set(map(json.dumps, old['enum'])).issubset(set(map(json.dumps, new.get('enum', [])))):
            changes.append(pointer + ': enum narrowed')
        if set(new.get('required', [])) != set(old.get('required', [])):
            changes.append(pointer + ': required field set changed')
        for key, schema in old.get('properties', {}).items():
            if key not in new.get('properties', {}): changes.append(pointer + '/properties/' + key + ': field removed')
            else: compare(schema, new['properties'][key], pointer + '/properties/' + key)
        if old.get('additionalProperties', True) is not False and new.get('additionalProperties', True) is False:
            changes.append(pointer + ': additional properties narrowed')
        for key in ('minimum', 'minLength', 'minItems'):
            if key in new and (key not in old or new[key] > old[key]): changes.append(pointer + ': ' + key + ' narrowed')
        for key in ('maximum', 'maxLength', 'maxItems'):
            if key in new and (key not in old or new[key] < old[key]): changes.append(pointer + ': ' + key + ' narrowed')
        if 'items' in old:
            if 'items' not in new: changes.append(pointer + ': array items removed')
            else: compare(old['items'], new['items'], pointer + '/items')
        for key in ('anyOf', 'oneOf', 'allOf', 'not', 'if', 'then', 'else', 'unevaluatedProperties'):
            if old.get(key) != new.get(key): changes.append(pointer + ': ' + key + ' changed; compatibility not proven')
        understood = {'$ref','type','format','pattern','contentEncoding','enum','required','properties','additionalProperties','minimum','minLength','minItems','maximum','maxLength','maxItems','items','anyOf','oneOf','allOf','not','if','then','else','unevaluatedProperties','$id','$schema','title','description','examples','default','deprecated','readOnly','writeOnly'}
        for key in (set(old) | set(new)) - understood:
            if old.get(key) != new.get(key): changes.append(pointer + ': unsupported constraint ' + key + ' changed')
    for name, schema in before.get('$defs', {}).items():
        if name not in after.get('$defs', {}): changes.append('/$defs/' + name + ': schema removed')
        else: compare(schema, after['$defs'][name], '/$defs/' + name)
    return sorted(set(changes))


def require_window(changes, before_major, after_major, window):
    if not changes: return
    if after_major <= before_major: raise ValueError('Breaking changes require a new major')
    if not isinstance(window, dict) or window.get('fromMajor') != before_major or window.get('toMajor') != after_major:
        raise ValueError('Breaking changes require an explicit matching compatibility window')
    start = datetime.datetime.fromisoformat(window['startsAt'].replace('Z', '+00:00'))
    end = datetime.datetime.fromisoformat(window['endsAt'].replace('Z', '+00:00'))
    if start.tzinfo is None or end.tzinfo is None or end <= start: raise ValueError('Invalid compatibility window')
    if window.get('retainedVersions') != [before_major, after_major]: raise ValueError('Both versioned interfaces must remain available')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('before', type=pathlib.Path); parser.add_argument('after', type=pathlib.Path)
    parser.add_argument('--before-major', required=True, type=int); parser.add_argument('--after-major', required=True, type=int)
    parser.add_argument('--window', type=pathlib.Path)
    args = parser.parse_args()
    changes = breaking_changes(json.loads(args.before.read_bytes()), json.loads(args.after.read_bytes()))
    require_window(changes, args.before_major, args.after_major, json.loads(args.window.read_bytes()) if args.window else None)
    print(json.dumps({'breakingChanges': changes, 'accepted': True}, sort_keys=True))

if __name__ == '__main__': main()
