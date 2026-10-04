#!/usr/bin/env python3
import argparse
import json
from pathlib import Path
import re


def operations(path):
    source = path.read_text()
    rows = []
    for block in re.split(r'(?=^\[)', source, flags=re.M):
        match = re.match(r'\[transport\.agent_http\.operation\.([a-z0-9_.-]+)\]\n', block)
        if not match:
            continue
        fields = {}
        for line in block[match.end():].splitlines():
            entry = re.fullmatch(r'([a-z_]+)\s*=\s*(".*")', line)
            if entry:
                if entry[1] in fields:
                    raise ValueError('duplicate operation declaration')
                fields[entry[1]] = json.loads(entry[2])
        name = match[1]
        access = fields['access']
        if access not in ('read', 'mutation'):
            raise ValueError('unknown operation access')
        variant = ''.join(part[0].upper() + part[1:] for part in re.split(r'[._-]', name))
        rows.append((variant, name, access == 'mutation'))
    rows.sort(key=lambda row: row[1])
    if not rows or len({row[0] for row in rows}) != len(rows):
        raise ValueError('missing or colliding operations')
    return rows


def render(rows):
    out = ['//! Code generated from the `LayerX` Agent API schema. DO NOT EDIT.', '',
           '#[derive(Clone, Copy, Debug, Eq, PartialEq)]', 'pub enum Operation {']
    out += ['    ' + variant + ',' for variant, _, _ in rows]
    out += ['}', '', 'impl Operation {', "    pub const ALL: &'static [Self] = &["]
    out += ['        Self::' + variant + ',' for variant, _, _ in rows]
    out += ['    ];', '', '    #[must_use]', "    pub const fn name(self) -> &'static str {", '        match self {']
    out += ['            Self::' + variant + ' => ' + json.dumps(name) + ',' for variant, name, _ in rows]
    out += ['        }', '    }', '', '    #[must_use]', '    pub const fn mutating(self) -> bool {',
            '        matches!(', '            self,']
    mutations = [variant for variant, _, mutating in rows if mutating]
    out += [('            ' if index == 0 else '                | ') + 'Self::' + variant for index, variant in enumerate(mutations)]
    out += ['        )', '    }', '}']
    return '\n'.join(out) + '\n'


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--schema', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    rows = operations(args.schema)
    existing = args.output.read_text()
    legacy_rows = [row for row in rows if row[1] != 'tenant.readiness']
    if existing not in (render(legacy_rows), render(rows)):
        raise ValueError('existing generated operation names or mutation classification differ')
    expected = render(rows)
    if args.check:
        if existing != expected:
            raise ValueError('generated operation output differs')
    else:
        args.output.write_text(expected)


if __name__ == '__main__':
    main()
